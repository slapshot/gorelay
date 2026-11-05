// Package delivery sends versioned, HMAC-signed events to operator-configured
// destinations. No URL in an inbound event is ever interpreted as a target.
package delivery

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/slapshot/gorelay/internal/queue"
)

type Target struct {
	ID            string
	URL           string
	Secret        []byte
	AllowLoopback bool // local development ONLY; explicit operator setting.
}

type Result struct {
	HTTPStatus int
	ErrorCode  string
	Retryable  bool
	RetryAfter time.Duration
	Success    bool
}

type Sender interface {
	Send(context.Context, queue.Job) Result
}

type Dispatcher struct {
	targets map[string]*endpoint
}

type endpoint struct {
	url    string
	secret []byte
	client *http.Client
}

func New(targets []Target, timeout time.Duration) (*Dispatcher,error) {
	if len(targets)==0 || timeout < time.Second || timeout > 20*time.Second {
		return nil,errors.New("invalid delivery configuration")
	}
	d:=&Dispatcher{targets:make(map[string]*endpoint,len(targets))}
	for _,t:=range targets{
		if t.ID=="" || d.targets[t.ID]!=nil || len(t.Secret)<32 {
			return nil,errors.New("invalid or duplicate delivery target")
		}
		u,err:=url.Parse(t.URL)
		if err!=nil || u.Hostname()=="" || u.User!=nil ||
			u.RawQuery!="" || u.Fragment!="" ||
			(u.Scheme!="https" && u.Scheme!="http") || (u.Path!="" && !strings.HasPrefix(u.Path,"/")) {
			return nil,errors.New("invalid destination URL")
		}
		if u.Port()!=""{
			port,err:=strconv.Atoi(u.Port())
			if err!=nil || port<1 || port>65535{return nil,errors.New("invalid destination port")}
		}
		if u.Scheme=="http" {
			ip:=net.ParseIP(u.Hostname())
			if !t.AllowLoopback || !((ip!=nil && ip.IsLoopback()) || strings.EqualFold(u.Hostname(),"localhost")) {
				return nil,errors.New("plain HTTP is only allowed to explicitly approved loopback targets")
			}
		}
		// Never use ProxyFromEnvironment; a proxy may circumvent the dial-time
		// IP policy. Disable keep-alives to revalidate on every new connection.
		tr:=&http.Transport{
			Proxy:nil,
			DialContext:secureDial(t.AllowLoopback,u.Scheme),
			TLSClientConfig:&tls.Config{MinVersion:tls.VersionTLS12},
			TLSHandshakeTimeout:4*time.Second,
			ResponseHeaderTimeout:timeout,
			DisableKeepAlives:true,
			MaxConnsPerHost:4,
		}
		d.targets[t.ID]=&endpoint{
			url:u.String(),
			secret:append([]byte(nil),t.Secret...),
			client:&http.Client{Transport:tr,Timeout:timeout,
				CheckRedirect:func(*http.Request,[]*http.Request)error{return http.ErrUseLastResponse}},
		}
	}
	return d,nil
}

func (d *Dispatcher) Close(){
	for _,target:=range d.targets {target.client.CloseIdleConnections()}
}

func (d *Dispatcher) Send(ctx context.Context,j queue.Job) Result {
	target,ok:=d.targets[j.DestinationID]
	if !ok{return Result{ErrorCode:"DESTINATION_NOT_CONFIGURED"}}
	envelope:=struct {
		ID string `json:"id"`
		Type string `json:"type"`
		Source string `json:"source"`
		Payload json.RawMessage `json:"payload"`
		CreatedAt string `json:"created_at"`
	}{j.ID,j.Type,j.ClientID,j.Payload,j.CreatedAt.UTC().Format(time.RFC3339Nano)}
	body,err:=json.Marshal(envelope)
	if err!=nil{return Result{ErrorCode:"INVALID_EVENT_ENVELOPE"}}
	now:=time.Now().UTC().Format(time.RFC3339)
	mac:=hmac.New(sha256.New,target.secret)
	_,_=io.WriteString(mac,now+".")
	_,_=mac.Write(body)
	signature:="v1="+hex.EncodeToString(mac.Sum(nil))
	req,err:=http.NewRequestWithContext(ctx,http.MethodPost,target.url,bytes.NewReader(body))
	if err!=nil{return Result{ErrorCode:"INVALID_DELIVERY_REQUEST"}}
	req.Header.Set("Content-Type","application/json")
	req.Header.Set("X-GoRelay-Event-ID",j.ID)
	req.Header.Set("X-GoRelay-Attempt",strconv.Itoa(j.Attempt))
	req.Header.Set("X-GoRelay-Timestamp",now)
	req.Header.Set("X-GoRelay-Signature",signature)
	response,err:=target.client.Do(req)
	if err!=nil{
		if errors.Is(err,errForbiddenIP) {
			return Result{ErrorCode:"FORBIDDEN_DESTINATION_IP"}
		}
		return Result{ErrorCode:"NETWORK_ERROR",Retryable:true}
	}
	defer response.Body.Close()
	_,_=io.Copy(io.Discard,io.LimitReader(response.Body,1024))
	s:=response.StatusCode
	if s>=200 && s<300 {return Result{Success:true,HTTPStatus:s}}
	if s==408 || s==425 || s==429 || s>=500{
		return Result{HTTPStatus:s,ErrorCode:"REMOTE_RETRYABLE",Retryable:true,
			RetryAfter:parseRetryAfter(response.Header.Get("Retry-After"))}
	}
	return Result{HTTPStatus:s,ErrorCode:"REMOTE_REJECTED"}
}

func parseRetryAfter(v string)time.Duration {
	if v=="" {return 0}
	if s,err:=strconv.ParseInt(strings.TrimSpace(v),10,64);err==nil{
		if s<=0{return 0}
		if s>3600{s=3600}
		return time.Duration(s)*time.Second
	}
	if at,err:=http.ParseTime(v);err==nil{
		d:=time.Until(at)
		if d<=0{return 0}
		if d>time.Hour{return time.Hour}
		return d
	}
	return 0
}

var errForbiddenIP=errors.New("forbidden outbound destination IP")

func secureDial(allowLoopback bool, scheme string)func(context.Context,string,string)(net.Conn,error){
	resolver:=net.DefaultResolver
	dialer:=&net.Dialer{Timeout:3*time.Second,KeepAlive:-1}
	return func(ctx context.Context, network, addr string)(net.Conn,error){
		host,port,err:=net.SplitHostPort(addr)
		if err!=nil{return nil,err}
		var ips []net.IP
		if ip:=net.ParseIP(host);ip!=nil{
			ips=[]net.IP{ip}
		}else{
			found,err:=resolver.LookupIPAddr(ctx,host)
			if err!=nil{return nil,err}
			for _,entry:=range found{ips=append(ips,entry.IP)}
		}
		if len(ips)==0{return nil,errForbiddenIP}
		// A mixed safe/unsafe answer fails closed; no re-resolution happens
		// in the underlying dialer because we pass a verified IP literal.
		for _,ip:=range ips{
			if !permittedIP(ip,allowLoopback,scheme){return nil,errForbiddenIP}
		}
		var lastErr error
		for _,ip:=range ips{
			conn,err:=dialer.DialContext(ctx,network,net.JoinHostPort(ip.String(),port))
			if err==nil{return conn,nil}
			lastErr=err
		}
		return nil,fmt.Errorf("cannot connect to approved destination: %w",lastErr)
	}
}

var nonPublicCIDRs=[]netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("2001:db8::/32"),
	netip.MustParsePrefix("2001::/23"),
}

func permittedIP(ip net.IP,allowLoopback bool,scheme string)bool{
	addr,ok:=netip.AddrFromSlice(ip)
	if !ok{return false}
	addr=addr.Unmap()
	if addr.IsLoopback() {
		return allowLoopback
	}
	if scheme!="https" || !addr.IsGlobalUnicast() || addr.IsPrivate() ||
		addr.IsLinkLocalUnicast() || addr.IsMulticast() || addr.IsUnspecified() {
		return false
	}
	for _,prefix:=range nonPublicCIDRs{
		if prefix.Contains(addr){return false}
	}
	return true
}

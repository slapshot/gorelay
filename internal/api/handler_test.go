package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/slapshot/gorelay/internal/domain"
	"github.com/slapshot/gorelay/internal/security"
)

type memoryStore struct {
	mu sync.Mutex
	keys map[string]domain.Input
	events map[string]domain.Event
	unavailable bool
}

func newMemoryStore() *memoryStore {
	return &memoryStore{keys: map[string]domain.Input{}, events: map[string]domain.Event{}}
}
func (s *memoryStore) Ready(context.Context) error {
	if s.unavailable { return domain.ErrUnavailable }
	return nil
}
func (s *memoryStore) Admit(_ context.Context, in domain.Input) (domain.Event, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.unavailable { return domain.Event{}, false, domain.ErrUnavailable }
	key := in.ClientID + "\x00" + in.IdempotencyKey
	if saved, ok := s.keys[key]; ok {
		if saved.Fingerprint != in.Fingerprint { return domain.Event{}, false, domain.ErrConflict }
		for _, event := range s.events {
			if event.ClientID == in.ClientID && event.Type == in.Type { return event, false, nil }
		}
	}
	id := fmt.Sprintf("00000000-0000-4000-8000-%012x", len(s.events)+1)
	e := domain.Event{ID:id, ClientID:in.ClientID, Type:in.Type, DestinationID:in.DestinationID, Status:domain.Pending, CreatedAt:time.Now().UTC()}
	s.keys[key] = in
	s.events[id] = e
	return e, true, nil
}
func (s *memoryStore) GetOwned(_ context.Context, clientID, id string) (domain.Event, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.events[id]
	if !ok || e.ClientID != clientID { return domain.Event{}, domain.ErrNotFound }
	if s.unavailable { return domain.Event{}, errors.New("database unreachable") }
	return e, nil
}

func testHandler(t *testing.T) (http.Handler, *memoryStore) {
	t.Helper()
	store := newMemoryStore()
	auth, err := security.NewAuthenticator([]security.Credential{
		{Client: security.Client{ID:"alpha", Destinations:map[string]bool{"sink":true}}, Token:strings.Repeat("a",40)},
		{Client: security.Client{ID:"beta", Destinations:map[string]bool{"sink":true}}, Token:strings.Repeat("b",40)},
	})
	if err != nil { t.Fatal(err) }
	server, err := New(store, auth, 1024)
	if err != nil { t.Fatal(err) }
	return server.Handler(), store
}

func post(handler http.Handler, token, key, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/v1/events", bytes.NewBufferString(body))
	req.Header.Set("Authorization","Bearer "+token)
	req.Header.Set("Idempotency-Key",key)
	req.Header.Set("Content-Type","application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec,req)
	return rec
}

func TestAdmissionIdempotencyAndStatus(t *testing.T) {
	handler,_ := testHandler(t)
	body := `{"type":"item.updated","destination_id":"sink","payload":{"count":2,"sku":"s1"}}`
	token := strings.Repeat("a",40)
	first := post(handler,token,"item-s1-v2",body)
	if first.Code != http.StatusAccepted { t.Fatalf("first admission: %d %s",first.Code,first.Body.String()) }
	var original struct{ ID string `json:"id"` }
	if err := json.Unmarshal(first.Body.Bytes(),&original);err != nil { t.Fatal(err) }
	second := post(handler,token,"item-s1-v2",`{"payload":{"sku":"s1","count":2},"destination_id":"sink","type":"item.updated"}`)
	if second.Code != http.StatusOK || !strings.Contains(second.Body.String(),original.ID) { t.Fatalf("duplicate: %d %s",second.Code,second.Body.String()) }
	if r := post(handler,token,"item-s1-v2",strings.Replace(body,`"count":2`,`"count":3`,1));r.Code != http.StatusConflict {
		t.Fatalf("conflict: %d",r.Code)
	}
	req := httptest.NewRequest(http.MethodGet,"/v1/events/"+original.ID,nil)
	req.Header.Set("Authorization","Bearer "+token)
	result := httptest.NewRecorder()
	handler.ServeHTTP(result,req)
	if result.Code != 200 || !strings.Contains(result.Body.String(),"PENDING"){t.Fatalf("GET result: %d %s",result.Code,result.Body.String())}
	req.Header.Set("Authorization","Bearer "+strings.Repeat("b",40))
	other := httptest.NewRecorder()
	handler.ServeHTTP(other,req)
	if other.Code != 404 { t.Fatalf("client isolation violated: %d",other.Code) }
}

func TestInvalidInput(t *testing.T) {
	h,_ := testHandler(t)
	token := strings.Repeat("a",40)
	cases := []struct{body string; code int}{
		{`{"type":"x","destination_id":"sink","payload":{},"extra":1}`,400},
		{`{"type":"x","type":"y","destination_id":"sink","payload":{}}`,400},
		{`{"type":"x","destination_id":"sink","payload":{"x":1,"x":2}}`,400},
		{`{"type":"x","destination_id":"other","payload":{}}`,403},
		{`{"type":"UPPER","destination_id":"sink","payload":{}}`,400},
		{`{"type":"x","destination_id":"sink","payload":[]}`,400},
		{strings.Repeat(" ",1200)+`{}`,413},
	}
	for _,tc := range cases {
		r:=post(h,token,"safe-key",tc.body)
		if r.Code!=tc.code {t.Errorf("body %q => %d expected %d (%s)",tc.body[:min(60,len(tc.body))],r.Code,tc.code,r.Body.String())}
	}
	if r:=post(h,"wrong","safe-key",`{"type":"x","destination_id":"sink","payload":{}}`);r.Code!=401 {
		t.Fatalf("bad auth: %d",r.Code)
	}
}

func TestConcurrentIdenticalAdmits(t *testing.T) {
	h,store:=testHandler(t)
	const n=50
	responses:=make(chan int,n)
	var wg sync.WaitGroup
	for i:=0;i<n;i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r:=post(h,strings.Repeat("a",40),"same-key",`{"type":"x","destination_id":"sink","payload":{"x":1}}`)
			responses<-r.Code
		}()
	}
	wg.Wait()
	close(responses)
	accepted:=0
	for code:=range responses {
		if code==202{accepted++} else if code!=200{t.Errorf("unexpected HTTP %d",code)}
	}
	if accepted!=1 || len(store.events)!=1 {t.Fatalf("accepted=%d events=%d",accepted,len(store.events))}
}

func TestReadyAndDraining(t *testing.T) {
	h,store:=testHandler(t)
	req:=httptest.NewRequest(http.MethodGet,"/readyz",nil)
	r:=httptest.NewRecorder();h.ServeHTTP(r,req)
	if r.Code!=200{t.Fatalf("ready %d",r.Code)}
	store.unavailable=true
	r=httptest.NewRecorder();h.ServeHTTP(r,req)
	if r.Code!=503{t.Fatalf("not-ready %d",r.Code)}
}

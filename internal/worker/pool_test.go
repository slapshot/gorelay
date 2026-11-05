package worker

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/slapshot/gorelay/internal/queue"
	"github.com/slapshot/gorelay/internal/delivery"
)

type fakeStore struct {
	done chan queue.Outcome
}
func (*fakeStore) Claim(context.Context,int,time.Duration)([]queue.Job,error){return nil,nil}
func (s *fakeStore) Finish(_ context.Context,_ queue.Job,r queue.Outcome)(bool,error){
	s.done<-r
	return true,nil
}
func (*fakeStore) ExpireExhausted(context.Context,int)(int,error){return 0,nil}

type fakeSender struct{result bool}
func (f fakeSender) Send(context.Context,queue.Job)delivery.Result{
	return delivery.Result{Success:f.result,HTTPStatus:204}
}

func TestRetryDelayBounds(t *testing.T){
	for n:=1;n<30;n++{
		d:=retryDelay(n,0)
		if d<100*time.Millisecond || d>5*time.Minute {t.Fatalf("retry %d: %s",n,d)}
	}
	if got:=retryDelay(1,10*time.Minute);got!=10*time.Minute{
		t.Fatalf("Retry-After override not respected: %s",got)
	}
	if got:=retryDelay(1,48*time.Hour);got!=time.Hour {t.Fatalf("retry after cap: %s",got)}
}

func TestPoolConfig(t *testing.T){
	log:=slog.New(slog.NewTextHandler(io.Discard,nil))
	s:=&fakeStore{done:make(chan queue.Outcome,1)}
	if _,err:=New(s,fakeSender{true},Config{Concurrency:1,HTTPTimeout:10*time.Second,Lease:12*time.Second,Poll:time.Second},log);err==nil {
		t.Fatal("accepted unsafe lease/HTTP timeout ratio")
	}
	if _,err:=New(s,fakeSender{true},Config{Concurrency:1,HTTPTimeout:10*time.Second,Lease:30*time.Second,Poll:time.Second},log);err!=nil{
		t.Fatal(err)
	}
}

// Package worker drives bounded concurrent HTTP delivery with durable
// claims and retry scheduling. Worker goroutines hold no DB locks during I/O.
package worker

import (
	"context"
	"errors"
	"log/slog"
	"math/rand/v2"
	"sync"
	"time"

	"github.com/slapshot/gorelay/internal/delivery"
	"github.com/slapshot/gorelay/internal/queue"
)

type Config struct {
	Concurrency int
	Lease       time.Duration
	HTTPTimeout time.Duration
	Poll        time.Duration
}

type Pool struct {
	store  queue.Store
	sender delivery.Sender
	cfg    Config
	logger *slog.Logger
}

func New(store queue.Store, sender delivery.Sender, cfg Config, logger *slog.Logger)(*Pool,error){
	if store==nil || sender==nil || logger==nil || cfg.Concurrency<1 || cfg.Concurrency>32 ||
		cfg.HTTPTimeout<time.Second || cfg.Lease<=cfg.HTTPTimeout+5*time.Second ||
		cfg.Lease>time.Hour || cfg.Poll<200*time.Millisecond || cfg.Poll>time.Minute{
		return nil,errors.New("invalid worker configuration")
	}
	return &Pool{store:store,sender:sender,cfg:cfg,logger:logger},nil
}

// Run exits after all worker routines observe ctx cancellation. An interrupted
// attempt remains durable and is eligible for reclaim after the lease expires.
func (p *Pool) Run(ctx context.Context) {
	var wg sync.WaitGroup
	for i:=0;i<p.cfg.Concurrency;i++{
		wg.Add(1)
		go func(workerID int){
			defer wg.Done()
			p.loop(ctx,workerID)
		}(i)
	}
	wg.Add(1)
	go func(){
		defer wg.Done()
		p.sweep(ctx)
	}()
	wg.Wait()
}

func (p *Pool) loop(ctx context.Context, workerID int){
	for ctx.Err()==nil{
		jobs,err:=p.store.Claim(ctx,1,p.cfg.Lease)
		if err!=nil {
			if ctx.Err()!=nil {return}
			p.logger.Warn("queue claim failed","worker",workerID)
			if !wait(ctx,3*time.Second){return}
			continue
		}
		if len(jobs)==0 {
			if !wait(ctx,p.cfg.Poll){return}
			continue
		}
		for _,job:=range jobs{
			if ctx.Err()!=nil{return}
			p.process(ctx,job)
		}
	}
}

func (p *Pool) process(ctx context.Context, job queue.Job){
	started:=time.Now()
	requestCtx,cancel:=context.WithTimeout(ctx,p.cfg.HTTPTimeout)
	result:=p.sender.Send(requestCtx,job)
	cancel()
	outcome:=queue.Outcome{HTTPStatus:result.HTTPStatus,ErrorCode:result.ErrorCode,
		Duration:time.Since(started)}
	switch{
	case result.Success:
		outcome.Kind=queue.Succeeded
	case result.Retryable:
		outcome.Kind=queue.Retryable
		outcome.RetryDelay=retryDelay(job.Attempt,result.RetryAfter)
	default:
		outcome.Kind=queue.Permanent
	}
	// Shutdown cancellation can prevent a safe DB write; the lease will expire
	// and reclaim rather than completing a result after ownership is lost.
	if ctx.Err()!=nil{return}
	completeCtx,cancelCompletion:=context.WithTimeout(ctx,3*time.Second)
	defer cancelCompletion()
	applied,err:=p.store.Finish(completeCtx,job,outcome)
	if err!=nil{
		p.logger.Warn("could not persist delivery result","event_id",job.ID,"attempt",job.Attempt)
		return
	}
	if !applied{
		p.logger.Warn("discarded stale delivery result","event_id",job.ID,"attempt",job.Attempt)
		return
	}
	p.logger.Info("delivery result","event_id",job.ID,"attempt",job.Attempt,"status",outcome.Kind,
		"http_status",result.HTTPStatus,"duration_ms",outcome.Duration.Milliseconds())
}

func (p *Pool) sweep(ctx context.Context){
	for ctx.Err()==nil{
		if _,err:=p.store.ExpireExhausted(ctx,64);err!=nil && ctx.Err()==nil{
			p.logger.Warn("could not expire exhausted leases")
		}
		if !wait(ctx,5*time.Second){return}
	}
}

func retryDelay(attempt int, retryAfter time.Duration) time.Duration{
	if attempt<1{attempt=1}
	ceiling:=2*time.Second
	for n:=1;n<attempt && ceiling<5*time.Minute;n++ {
		ceiling*=2
		if ceiling>5*time.Minute{ceiling=5*time.Minute}
	}
	// Full jitter; floor to avoid tight retry loops when jitter is near zero.
	delay:=time.Duration(rand.Int64N(int64(ceiling)))
	if delay<100*time.Millisecond{delay=100*time.Millisecond}
	if retryAfter>time.Hour{retryAfter=time.Hour}
	if retryAfter>delay{delay=retryAfter}
	return delay
}

func wait(ctx context.Context,d time.Duration)bool{
	timer:=time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():return false
	case <-timer.C:return true
	}
}

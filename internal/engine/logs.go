package engine

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// Log batching: lines are flushed every logFlushInterval or at logBatchSize
// lines, whichever comes first. Variables so tests can shrink them.
var (
	logFlushInterval = 100 * time.Millisecond
	logBatchSize     = 500
)

func (d *Detail) open(ctx context.Context, cs kubernetes.Interface, p LogsSubscribeParams, container string) (io.ReadCloser, error) {
	if d.Open != nil {
		return d.Open(ctx, cs, p, container)
	}
	o := &corev1.PodLogOptions{Container: container, Follow: p.Follow, Previous: p.Previous, Timestamps: p.Timestamps}
	if p.TailLines > 0 {
		n := int64(p.TailLines)
		o.TailLines = &n
	}
	return cs.CoreV1().Pods(p.Namespace).GetLogs(p.Pod, o).Stream(ctx)
}

func (d *Detail) logs(ctx context.Context, raw json.RawMessage) (func(Emit), error) {
	var p LogsSubscribeParams
	if err := decode(raw, &p); err != nil {
		return nil, err
	}
	if p.Context == "" || p.Namespace == "" || p.Pod == "" || p.TailLines < 0 {
		return nil, invalid("context, namespace and pod are required, tailLines must not be negative")
	}
	cn, err := d.Conns.Get(ctx, p.Context)
	if err != nil {
		return nil, err
	}
	cs, err := d.clientset(cn.Config)
	if err != nil {
		return nil, err
	}
	pod, err := cs.CoreV1().Pods(p.Namespace).Get(ctx, p.Pod, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	var names []string
	for _, c := range pod.Spec.Containers {
		names = append(names, c.Name)
	}
	switch {
	case p.AllContainers:
	case p.Container != "":
		names = []string{p.Container}
	case len(names) > 1:
		return nil, invalid("pod %s has several containers, choose one of: %s", p.Pod, strings.Join(names, ", "))
	default:
		names = []string{""}
	}
	prefix := p.AllContainers
	return func(emit Emit) { d.stream(ctx, cs, p, names, prefix, emit) }, nil
}

// stream reads every container's log, batches the lines and emits them, then
// emits end. It returns when all readers have stopped; nothing is emitted
// once ctx is done.
func (d *Detail) stream(ctx context.Context, cs kubernetes.Interface, p LogsSubscribeParams, names []string, prefix bool, emit Emit) {
	lines := make(chan string)
	var wg sync.WaitGroup
	var mu sync.Mutex
	var firstErr error
	for _, name := range names {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := d.read(ctx, cs, p, name, prefix, lines); err != nil && ctx.Err() == nil {
				mu.Lock()
				if firstErr == nil {
					firstErr = err
				}
				mu.Unlock()
			}
		}()
	}
	go func() { wg.Wait(); close(lines) }()

	var batch []string
	flush := func() {
		if len(batch) > 0 && ctx.Err() == nil {
			emit(EventLines, Lines{Lines: batch})
		}
		batch = nil
	}
	tick := time.NewTicker(logFlushInterval)
	defer tick.Stop()
loop:
	for {
		select {
		case l, ok := <-lines:
			if !ok {
				break loop
			}
			batch = append(batch, l)
			if len(batch) >= logBatchSize {
				flush()
			}
		case <-tick.C:
			flush()
		case <-ctx.Done():
			break loop
		}
	}
	// Readers are stopped by ctx (their streams are closed) or have finished.
	wg.Wait()
	if ctx.Err() != nil {
		return
	}
	flush()
	end := End{}
	if firstErr != nil {
		end.Error = redact(firstErr.Error())
	}
	emit(EventEnd, end)
}

func (d *Detail) read(ctx context.Context, cs kubernetes.Interface, p LogsSubscribeParams, container string, prefix bool, out chan<- string) error {
	rc, err := d.open(ctx, cs, p, container)
	if err != nil {
		return err
	}
	defer rc.Close()
	stop := context.AfterFunc(ctx, func() { rc.Close() }) // unblocks Read
	defer stop()
	pre := ""
	if prefix {
		pre = "[" + container + "] "
	}
	br := bufio.NewReader(rc)
	for {
		l, err := br.ReadString('\n')
		if l = strings.TrimRight(l, "\r\n"); l != "" || err == nil {
			select {
			case out <- pre + l:
			case <-ctx.Done():
				return nil
			}
		}
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

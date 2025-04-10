package proxy

import (
	"fmt"
	"log"
	"net/http"
	"sync"
)

type pipelineItem interface{ *http.Request | *http.Response }

type ReadOnlyHook[I pipelineItem] func(I) error

// queueItem represents an item in the hooks processing queue
type queueItem[I pipelineItem] struct {
	req   I
	hooks []ReadOnlyHook[I]
}

type readOnlyPipeline[I pipelineItem] struct {
	hooks      []ReadOnlyHook[I]
	hooksMutex sync.RWMutex
	queue      chan queueItem[I]
}

func newReadOnlyPipeline[I pipelineItem](hooks []ReadOnlyHook[I]) *readOnlyPipeline[I] {
	pipeline := &readOnlyPipeline[I]{
		hooks:      append([]ReadOnlyHook[I]{}, hooks...),
		hooksMutex: sync.RWMutex{},

		queue: make(chan queueItem[I], 1000),
	}

	go pipeline.processPipelineQueue()
	return pipeline
}

// processPipelineQueue runs in a goroutine to process items from the pipeline queue
func (p *readOnlyPipeline[I]) processPipelineQueue() {
	for item := range p.queue {
		p.processItem(item)
	}
}

// processRequestPipelineItem processes a single request pipeline item asynchronously and concurrently
func (p *readOnlyPipeline[I]) processItem(item queueItem[I]) {
	hooks := item.hooks
	req := item.req

	if len(hooks) == 0 {
		return
	}

	var wg sync.WaitGroup
	errChan := make(chan error, len(hooks))

	// Launch each hook function concurrently
	for _, fn := range hooks {
		wg.Add(1)
		go func(f ReadOnlyHook[I]) {
			defer wg.Done()
			tempReq := clone(req)
			if err := f(tempReq); err != nil {
				errChan <- err
			}
		}(fn)
	}

	// Wait for all goroutines to complete
	wg.Wait()
	close(errChan)

	// Log any errors that occurred
	for err := range errChan {
		if err != nil {
			log.Printf("Error processing pipeline: %v", err)
		}
	}
}

func (p *readOnlyPipeline[I]) runPipeline(r I) error {
	p.hooksMutex.RLock()
	hooks := p.hooks
	p.hooksMutex.RUnlock()

	if len(hooks) > 0 {
		select {
		case p.queue <- queueItem[I]{
			req:   r,
			hooks: hooks,
		}:
		default:
			return fmt.Errorf("Pipeline queue full")
		}
	}

	return nil
}

func (p *readOnlyPipeline[I]) setHooks(hooks []ReadOnlyHook[I]) {
	p.hooksMutex.Lock()
	p.hooks = append([]ReadOnlyHook[I]{}, hooks...)
	p.hooksMutex.Unlock()
}

func clone[I pipelineItem](r I) I {
	switch v := any(r).(type) {
	case *http.Request:
		return any(cloneRequest(v)).(I)
	case *http.Response:
		return any(cloneResponse(v)).(I)
	}

	panic("invalid type used in clone function")
}

package pipeline

import (
	"fmt"
	"log"
	"net/http"
	"sync"

	"github.com/artilugio0/proxy-vibes/internal/httpbytes"
)

// ReadOnlyHook defines a hook that processes an item without modifying it.
type ReadOnlyHook[I PipelineItem] func(I) error

// ModHook defines a hook that can modify an item and return it.
type ModHook[I PipelineItem] func(I) (I, error)

// PipelineItem constrains the types that can be processed by the pipelines.
type PipelineItem interface{ *http.Request | *http.Response }

// roQueueItem represents an item in the read-only pipeline's processing queue.
type roQueueItem[I PipelineItem] struct {
	req   I
	hooks []ReadOnlyHook[I]
}

// ReadOnlyPipeline manages a pipeline of read-only hooks processed asynchronously.
type ReadOnlyPipeline[I PipelineItem] struct {
	hooks      []ReadOnlyHook[I]
	hooksMutex sync.RWMutex
	queue      chan roQueueItem[I]
}

// NewReadOnlyPipeline initializes a new read-only pipeline with the given hooks.
func NewReadOnlyPipeline[I PipelineItem](hooks []ReadOnlyHook[I]) *ReadOnlyPipeline[I] {
	pipeline := &ReadOnlyPipeline[I]{
		hooks:      append([]ReadOnlyHook[I]{}, hooks...), // Defensive copy of hooks
		hooksMutex: sync.RWMutex{},
		queue:      make(chan roQueueItem[I], 1000), // Buffer size of 1000
	}

	go pipeline.processPipelineQueue()
	return pipeline
}

// processPipelineQueue runs in a goroutine to process items from the queue.
func (p *ReadOnlyPipeline[I]) processPipelineQueue() {
	for item := range p.queue {
		p.processItem(item)
	}
}

// processItem processes a single pipeline item by applying all hooks concurrently.
func (p *ReadOnlyPipeline[I]) processItem(item roQueueItem[I]) {
	hooks := item.hooks
	req := item.req

	if len(hooks) == 0 {
		return
	}

	var wg sync.WaitGroup
	errChan := make(chan error, len(hooks))

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

	wg.Wait()
	close(errChan)

	// Log any errors from hook execution
	for err := range errChan {
		if err != nil {
			log.Printf("Error processing pipeline: %v", err)
		}
	}
}

// RunPipeline queues an item for processing in the read-only pipeline.
func (p *ReadOnlyPipeline[I]) RunPipeline(r I) error {
	p.hooksMutex.RLock()
	hooks := p.hooks
	p.hooksMutex.RUnlock()

	if len(hooks) > 0 {
		select {
		case p.queue <- roQueueItem[I]{req: r, hooks: hooks}:
			// Successfully queued
		default:
			return fmt.Errorf("pipeline queue full")
		}
	}
	return nil
}

// SetHooks updates the hooks in the read-only pipeline.
func (p *ReadOnlyPipeline[I]) SetHooks(hooks []ReadOnlyHook[I]) {
	p.hooksMutex.Lock()
	p.hooks = append([]ReadOnlyHook[I]{}, hooks...) // Defensive copy
	p.hooksMutex.Unlock()
}

// ModPipeline manages a pipeline of modification hooks processed synchronously.
type ModPipeline[I PipelineItem] struct {
	hooks      []ModHook[I]
	hooksMutex sync.RWMutex
}

// NewModPipeline initializes a new modification pipeline with the given hooks.
func NewModPipeline[I PipelineItem](hooks []ModHook[I]) *ModPipeline[I] {
	return &ModPipeline[I]{
		hooks:      append([]ModHook[I]{}, hooks...), // Defensive copy
		hooksMutex: sync.RWMutex{},
	}
}

// RunPipeline applies all modification hooks sequentially to the item.
func (p *ModPipeline[I]) RunPipeline(r I) (I, error) {
	p.hooksMutex.RLock()
	hooks := p.hooks
	p.hooksMutex.RUnlock()

	for _, fn := range hooks {
		modifiedReq, err := fn(r)
		if err != nil {
			return r, err
		}
		r = modifiedReq

		// Handle body reset or cloning based on type
		switch v := any(r).(type) {
		case *http.Request:
			if body, ok := v.Body.(*httpbytes.BodyWrapper); ok {
				body.Reset()
			} else {
				r = clone(r)
			}
		case *http.Response:
			if body, ok := v.Body.(*httpbytes.BodyWrapper); ok {
				body.Reset()
			} else {
				r = clone(r)
			}
		}
	}
	return r, nil
}

// SetHooks updates the hooks in the modification pipeline.
func (p *ModPipeline[I]) SetHooks(hooks []ModHook[I]) {
	p.hooksMutex.Lock()
	p.hooks = append([]ModHook[I]{}, hooks...) // Defensive copy
	p.hooksMutex.Unlock()
}

// clone creates a copy of the pipeline item to prevent unintended modifications.
func clone[I PipelineItem](r I) I {
	switch v := any(r).(type) {
	case *http.Request:
		return any(httpbytes.CloneRequest(v)).(I)
	case *http.Response:
		return any(httpbytes.CloneResponse(v)).(I)
	default:
		panic(fmt.Sprintf("Error: invalid type in clone function: %T", r))
	}
}

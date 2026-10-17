package tools

import (
	"context"
	"sync"
)

// AttachedImage is one image a tool wants the next model call to see.
// The transcript stores the tool's text; the bytes travel only on that call.
type AttachedImage struct {
	MIME string
	Data []byte
}

type sinkKey struct{}

type imageSink struct {
	mu   sync.Mutex
	imgs []AttachedImage
}

// WithImageSink returns a context the tool can attach images to, and the sink
// the caller reads after Run. Each call gets its own sink.
func WithImageSink(ctx context.Context) (context.Context, *imageSink) {
	s := &imageSink{}
	return context.WithValue(ctx, sinkKey{}, s), s
}

func (s *imageSink) add(img AttachedImage) {
	if s == nil || len(img.Data) == 0 {
		return
	}
	s.mu.Lock()
	s.imgs = append(s.imgs, img)
	s.mu.Unlock()
}

// Take returns the images attached during Run.
func (s *imageSink) Take() []AttachedImage {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	out := append([]AttachedImage(nil), s.imgs...)
	return out
}

// AttachImage adds one image to the sink on ctx. Tools call it from Run.
// A context without a sink drops the image.
func AttachImage(ctx context.Context, img AttachedImage) {
	sinkFrom(ctx).add(img)
}

func sinkFrom(ctx context.Context) *imageSink {
	s, _ := ctx.Value(sinkKey{}).(*imageSink)
	return s
}

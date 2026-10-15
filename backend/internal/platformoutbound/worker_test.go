package platformoutbound

import (
	"errors"
	"testing"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/platform"
	"github.com/riverqueue/river"
)

func TestWorkerInvalidJobAndRetryDeadline(t *testing.T) {
	if _, err := NewWorker(nil); err == nil {
		t.Fatal("nil executor accepted")
	}
	w := &Worker{}
	for _, job := range []*river.Job[platform.ChatSendArgs]{nil, {Args: platform.ChatSendArgs{}}, {Args: platform.ChatSendArgs{Deadline: 1000000000000}}} {
		var cancel *river.JobCancelError
		if err := w.Work(t.Context(), job); !errors.As(err, &cancel) {
			t.Fatal("invalid job not cancelled", err)
		}
	}
	for _, deadline := range []int64{time.Now().Add(time.Minute).Unix(), time.Now().Unix(), time.Now().Add(-time.Minute).Unix()} {
		job := &river.Job[platform.ChatSendArgs]{Args: platform.ChatSendArgs{Deadline: deadline}}
		if retry := w.NextRetry(job); retry.After(time.Unix(deadline, 0)) || retry.After(time.Now().Add(1100*time.Millisecond)) {
			t.Fatal("retry exceeds deadline", retry)
		}
	}
}

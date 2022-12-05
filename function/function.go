package function

import (
	"context"
	"net/http"

	"github.com/GoogleCloudPlatform/functions-framework-go/functions"
	"github.com/cloudevents/sdk-go/v2/event"
)

func init() {
	functions.HTTP("receive", receive)
	functions.CloudEvent("process", process)
	functions.CloudEvent("send", process)
}

func receive(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("receive"))
}

func process(ctx context.Context, evt event.Event) error {
	return nil
}

func send(ctx context.Context, evt event.Event) error {
	return nil
}

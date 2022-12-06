package function

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/http/httputil"
	"os"

	"cloud.google.com/go/pubsub"
	"github.com/GoogleCloudPlatform/functions-framework-go/functions"
	"github.com/cloudevents/sdk-go/v2/event"
)

func init() {
	functions.HTTP("receive", receive)
	functions.CloudEvent("process", process)
	functions.CloudEvent("send", send)
}

type processMessage struct {
	Dummy string
}

type sendMessage struct {
	Dummy string
}

func receive(w http.ResponseWriter, r *http.Request) {
	log.Printf("receive")
	reqBytes, err := httputil.DumpRequest(r, true)
	if err != nil {
		returnError(w, http.StatusInternalServerError, err)
		return
	}
	log.Printf("request: %s", string(reqBytes))

	ctx := r.Context()
	projectID := os.Getenv("PROJECT_ID")
	waitProcessTopic := os.Getenv("WAIT_PROCESS_TOPIC")

	msg := processMessage{Dummy: "dummy"}
	msgBytes, err := json.Marshal(msg)
	if err != nil {
		returnError(w, http.StatusInternalServerError, err)
		return
	}

	client, err := pubsub.NewClient(ctx, projectID)
	if err != nil {
		returnError(w, http.StatusInternalServerError, err)
		return
	}
	defer client.Close()
	topic := client.Topic(waitProcessTopic)
	result := topic.Publish(ctx, &pubsub.Message{Data: msgBytes})
	id, err := result.Get(ctx)
	if err != nil {
		returnError(w, http.StatusInternalServerError, err)
		return
	}
	log.Printf("publish: %s", id)

	w.WriteHeader(http.StatusOK)
	w.Write([]byte("receive"))
}

func process(ctx context.Context, evt event.Event) error {
	log.Printf("process")
	log.Printf("request: %v", evt)

	projectID := os.Getenv("PROJECT_ID")
	waitSendTopic := os.Getenv("WAIT_SEND_TOPIC")

	msg := sendMessage{Dummy: "dummy"}
	msgBytes, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("json.Marshal failed; %w", err)
	}

	client, err := pubsub.NewClient(ctx, projectID)
	if err != nil {
		return fmt.Errorf("pubsub.NewClient failed; %w", err)
	}
	defer client.Close()
	topic := client.Topic(waitSendTopic)
	result := topic.Publish(ctx, &pubsub.Message{Data: msgBytes})
	id, err := result.Get(ctx)
	if err != nil {
		return fmt.Errorf("")
	}
	log.Printf("publish: %s", id)

	return nil
}

func send(ctx context.Context, evt event.Event) error {
	log.Printf("send")
	log.Printf("request: %v", evt)
	return nil
}

func returnError(w http.ResponseWriter, code int, err error) {
	log.Printf("error: %v", err.Error())
	w.WriteHeader(code)
	if _, err := w.Write([]byte(err.Error())); err != nil {
		log.Printf("http.ResponseWriter.Write failed; %v", err.Error())
	}
}

package function

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httputil"
	"os"
	"strings"

	"cloud.google.com/go/pubsub"
	vision "cloud.google.com/go/vision/apiv1"
	"github.com/GoogleCloudPlatform/functions-framework-go/functions"
	"github.com/cloudevents/sdk-go/v2/event"

	secretmanager "cloud.google.com/go/secretmanager/apiv1"
	"cloud.google.com/go/secretmanager/apiv1/secretmanagerpb"
)

func init() {
	functions.HTTP("receive", receive)
	functions.CloudEvent("process", process)
	functions.CloudEvent("send", send)
}

type lineWebHook struct {
	Events []lineEvent `json:"events"`
}

type lineEvent struct {
	ReplyToken       string           `json:"replyToken"`
	LineEventMessage lineEventMessage `json:"message"`
}

type lineEventMessage struct {
	ID string `json:"id"`
}

type processMessage struct {
	ImageID    string
	ReplyToken string
}

type sendMessage struct {
	ReplyToken string
	Labels     []string
}

type messagePublishedData struct {
	Message pubSubMessage
}

type pubSubMessage struct {
	Data []byte `json:"data"`
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

	reqBody, err := io.ReadAll(r.Body)
	if err != nil {
		returnError(w, http.StatusInternalServerError, err)
		return
	}
	var lineWebHook lineWebHook
	if err := json.Unmarshal(reqBody, &lineWebHook); err != nil {
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
	for _, evt := range lineWebHook.Events {
		msg := processMessage{ImageID: evt.LineEventMessage.ID, ReplyToken: evt.ReplyToken}
		msgBytes, err := json.Marshal(msg)
		if err != nil {
			returnError(w, http.StatusInternalServerError, err)
			return
		}
		result := topic.Publish(ctx, &pubsub.Message{Data: msgBytes})
		id, err := result.Get(ctx)
		if err != nil {
			returnError(w, http.StatusInternalServerError, err)
			return
		}
		log.Printf("publish: %s", id)
	}
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("receive"))
}

func process(ctx context.Context, evt event.Event) error {
	log.Printf("process")
	log.Printf("request: %v", evt)

	projectID := os.Getenv("PROJECT_ID")
	waitSendTopic := os.Getenv("WAIT_SEND_TOPIC")

	var subMsg messagePublishedData
	if err := evt.DataAs(&subMsg); err != nil {
		return fmt.Errorf("event.Event.DataAs failed; %w", err)
	}
	var procMsg processMessage
	if err := json.Unmarshal(subMsg.Message.Data, &procMsg); err != nil {
		return fmt.Errorf("json.Unmarshal failed; %w", err)
	}

	log.Printf("image ID: %s", procMsg.ImageID)
	log.Printf("reply token: %s", procMsg.ReplyToken)

	channelAccessToken, err := getSecret(ctx, projectID, "channel-access-token")
	if err != nil {
		return err
	}
	log.Print("get secret")

	image, err := downloadImage(channelAccessToken, procMsg.ImageID)
	if err != nil {
		return err
	}
	log.Print("download image")

	labels, err := analyzeImage(ctx, image)
	if err != nil {
		return err
	}
	log.Printf("labels: %v\n", labels)

	msg := sendMessage{ReplyToken: procMsg.ReplyToken, Labels: labels}
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

	projectID := os.Getenv("PROJECT_ID")

	var subMsg messagePublishedData
	if err := evt.DataAs(&subMsg); err != nil {
		return fmt.Errorf("event.Event.DataAs failed; %w", err)
	}
	var sendMsg sendMessage
	if err := json.Unmarshal(subMsg.Message.Data, &sendMsg); err != nil {
		return fmt.Errorf("json.Unmarshal failed; %w", err)
	}

	log.Printf("reply token: %s", sendMsg.ReplyToken)
	log.Printf("labels: %v", sendMsg.Labels)

	text := strings.Join(sendMsg.Labels, "\n")

	channelAccessToken, err := getSecret(ctx, projectID, "channel-access-token")
	if err != nil {
		return err
	}
	log.Print("get secret")

	if err := sendReply(channelAccessToken, sendMsg.ReplyToken, text); err != nil {
		return err
	}
	log.Print("send reply")

	return nil
}

func returnError(w http.ResponseWriter, code int, err error) {
	log.Printf("error: %v", err.Error())
	w.WriteHeader(code)
	if _, err := w.Write([]byte(err.Error())); err != nil {
		log.Printf("http.ResponseWriter.Write failed; %v", err.Error())
	}
}

func getSecret(ctx context.Context, projectID, secretName string) (string, error) {
	clt, err := secretmanager.NewClient(ctx)
	if err != nil {
		return "", fmt.Errorf("secretmanager.NewClient failed; %w", err)
	}
	defer clt.Close()
	req := &secretmanagerpb.AccessSecretVersionRequest{
		Name: fmt.Sprintf("projects/%s/secrets/%s/versions/latest", projectID, secretName),
	}
	resp, err := clt.AccessSecretVersion(ctx, req)
	if err != nil {
		return "", fmt.Errorf("secretmanager.Client.AccessSecretVersion failed; %w", err)
	}
	return string(resp.Payload.Data), nil
}

func downloadImage(channelAccessToken, imageID string) ([]byte, error) {
	url := fmt.Sprintf("https://api-data.line.me/v2/bot/message/%s/content", imageID)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("http.NewRequest failed; %w", err)
	}
	req.Header = make(http.Header)
	req.Header.Add("Authorization", fmt.Sprintf("Bearer %s", channelAccessToken))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http.DefaultClient.Get failed; %w", err)
	}
	defer resp.Body.Close()
	respBytes, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, fmt.Errorf("io.ReadAll failed; %w", err)
	}
	return respBytes, nil
}

func analyzeImage(ctx context.Context, imageBytes []byte) ([]string, error) {
	client, err := vision.NewImageAnnotatorClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("vision.NewImageAnnotatorClient failed; %w", err)
	}
	defer client.Close()
	image, err := vision.NewImageFromReader(bytes.NewReader(imageBytes))
	if err != nil {
		return nil, fmt.Errorf("vision.NewImageFromReader failed; %w", err)
	}
	labels, err := client.DetectLabels(ctx, image, nil, 10)
	if err != nil {
		return nil, fmt.Errorf("vision.ImageAnnotatorClient.DetectLabels failed; %w", err)
	}
	results := []string{}
	for _, label := range labels {
		results = append(results, label.Description)
	}
	return results, nil
}

type replyFormat struct {
	ReplyToken string               `json:"replyToken"`
	Messages   []replyFormatMessage `json:"messages"`
}

type replyFormatMessage struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

func sendReply(channelAccessToken, replyToken, text string) error {
	url := "https://api.line.me/v2/bot/message/reply"
	reply := replyFormat{
		ReplyToken: replyToken,
		Messages: []replyFormatMessage{{
			Type: "text",
			Text: text,
		}},
	}
	replyBytes, err := json.Marshal(reply)
	if err != nil {
		return fmt.Errorf("json.Marshal failed; %w", err)
	}
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(replyBytes))
	if err != nil {
		return fmt.Errorf("http.NewRequest failed; %w", err)
	}
	req.Header = make(http.Header)
	req.Header.Add("Authorization", fmt.Sprintf("Bearer %s", channelAccessToken))
	req.Header.Add("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("http.DefaultClient.Get failed; %w", err)
	}
	defer resp.Body.Close()
	return nil
}

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/dchest/uniuri"
	tasks "github.com/help-me-someone/scalable-p2-tasks"
	"github.com/hibiken/asynq"
)

const (
	region = "sgp1"
)

func main() {
	taskQueueHandler := &TaskQueueHandler{
		Connection: asynq.NewClient(asynq.RedisClientOpt{
			Addr: "redis:6379",
		}),
	}

	http.HandleFunc("/upload", GetUploadPresignedUrl)
	http.HandleFunc("/save", taskQueueHandler.TaskMiddleware(HandleVideoSave))
	log.Println("Server started successfully, listening on port 7000.")
	log.Fatal(http.ListenAndServe(":7000", nil))
}

func enableCors(w *http.ResponseWriter) {
	// Enable cors. This isn't good.
	(*w).Header().Set("Access-Control-Allow-Origin", "*")
	(*w).Header().Set("Access-Control-Allow-Headers", "Content-Type")
}

type TaskQueueHandler struct {
	Connection *asynq.Client
}

func (t *TaskQueueHandler) TaskMiddleware(next func(http.ResponseWriter, *http.Request)) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := context.WithValue(r.Context(), "queue_conn", t.Connection)
		next(w, r.WithContext(ctx))
	}
}

// This API kickstarts the pipeline for saving
func HandleVideoSave(w http.ResponseWriter, r *http.Request) {
	// Ensure the method is correct.
	if r.Method != "POST" {
		log.Println("Error: Not GET request")
		w.WriteHeader(http.StatusBadRequest)
		resp := map[string]interface{}{
			"success": false,
			"message": "invalid method",
		}
		json.NewEncoder(w).Encode(resp)
		return
	}

	log.Println("Handling save...")
	log.Println("X-Username:", r.Header.Get("X-Username"))

	// Enable cors.
	enableCors(&w)

	// Get the connection
	qc := r.Context().Value("queue_conn")
	if qc == nil {
		log.Println("Error: queue connection not specified")
		w.WriteHeader(http.StatusInternalServerError)
		resp := map[string]interface{}{
			"success": false,
			"message": "queue connection not specified",
		}
		json.NewEncoder(w).Encode(resp)
		return
	}
	queueConn, ok := qc.(*asynq.Client)
	if !ok {
		log.Println("Error: queue connection invalid type")
		w.WriteHeader(http.StatusBadRequest)
		resp := map[string]interface{}{
			"success": false,
			"message": "queue connection invalid type",
		}
		json.NewEncoder(w).Encode(resp)
		return
	}

	// The Forwardauth should send this.
	// NOTE: See IsAuth in auth svc.
	user := r.Header.Get("X-Username")
	if len(user) == 0 {
		log.Println("No X-Username found in header.")
		user = "user"
	}

	// Should be set by the request.
	video_name := r.Header.Get("X-Video-Name")
	if len(video_name) == 0 {
		log.Println("No X-Video-Name found in header.")
		video_name = "video-name"
	}

	// Create the task.
	t1, err := tasks.NewVideoSaveTask(user, video_name)
	if err != nil {
		log.Println("Error: failed to create task:", err)
		w.WriteHeader(http.StatusBadRequest)
		resp := map[string]interface{}{
			"success": false,
			"message": "failed to create task",
		}
		json.NewEncoder(w).Encode(resp)
		return
	}

	// Queue the task.
	info, err := queueConn.Enqueue(t1)
	if err != nil {
		log.Println("Error: failed to queue task:", err)
		w.WriteHeader(http.StatusBadRequest)
		resp := map[string]interface{}{
			"success": false,
			"message": "failed to queue task",
		}
		json.NewEncoder(w).Encode(resp)
		return
	}

	resp := map[string]interface{}{
		"success": true,
		"message": "sucessfully enqueued task",
		"type":    info.Type,
		"user":    user,
		"video":   video_name, // This is the video address in the bucket.
	}
	json.NewEncoder(w).Encode(resp)
	log.Printf(" [*] Successfully enqueued task: %+v", info)
}

func GetUploadPresignedUrl(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		log.Println("Error: Not GET request")
		w.WriteHeader(http.StatusBadRequest)
		resp := map[string]interface{}{
			"success": false,
			"message": "invalid method",
		}
		json.NewEncoder(w).Encode(resp)
		return
	}

	log.Println("User requesting presigned:", r.Header.Get("username"))

	// I really need to enable cors
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

	customResolver := aws.EndpointResolverWithOptionsFunc(func(service, region string, options ...interface{}) (aws.Endpoint, error) {
		return aws.Endpoint{
			URL: "https://" + region + ".digitaloceanspaces.com",
		}, nil
	})

	cfg, err := config.LoadDefaultConfig(context.TODO(),
		config.WithRegion(region),
		config.WithEndpointResolverWithOptions(customResolver),
	)

	if err != nil {
		log.Println("Error: Can't load config")
		w.WriteHeader(http.StatusBadRequest)
		resp := map[string]interface{}{
			"success": false,
			"message": "user not specified",
		}
		json.NewEncoder(w).Encode(resp)
		return
	}

	_, err = cfg.Credentials.Retrieve(context.TODO())
	if err != nil {
		log.Println("Error: No credentials set")
		w.WriteHeader(http.StatusBadRequest)
		resp := map[string]interface{}{
			"success": false,
			"message": "no credential found",
		}
		json.NewEncoder(w).Encode(resp)
	}

	// Create the client.
	client := s3.NewFromConfig(cfg)

	// Presign the client.
	presignClient := s3.NewPresignClient(client)

	// Create the random string we'll save the file to.
	randomKey := uniuri.NewLen(100)

	response, err := presignClient.PresignPutObject(context.TODO(), &s3.PutObjectInput{
		Bucket: aws.String("toktik-videos"),
		Key:    aws.String(randomKey),
	})

	if err != nil {
		log.Println("Can't retrieve pre-signed object")
		w.WriteHeader(http.StatusInternalServerError)
		resp := map[string]interface{}{
			"success": false,
			"message": "error retrieving pre-signed object",
		}
		json.NewEncoder(w).Encode(resp)
		return
	}

	fmt.Println("The URL: ", response.URL)
	resp := map[string]interface{}{
		"success": true,
		"url":     response.URL,
		"key":     randomKey,
	}
	json.NewEncoder(w).Encode(resp)
}

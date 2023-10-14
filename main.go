package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/dchest/uniuri"
	"github.com/hibiken/asynq"
)

const (
	region = "sgp1"
)

func main() {
	http.HandleFunc("/upload", GetUploadPresignedUrl)
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
	next       func(http.ResponseWriter, *http.Request)
}

func (t *TaskQueueHandler) TaskMiddleware(next func(http.ResponseWriter, *http.Request)) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := context.WithValue(r.Context(), "queue_conn", t.Connection)
		t.next(w, r.WithContext(ctx))
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

	_ = queueConn

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

	// I really need to enable cors
	enableCors(&w)

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

	expireDate := time.Now().AddDate(0, 0, 1)

	response, err := presignClient.PresignPutObject(context.TODO(), &s3.PutObjectInput{
		Bucket:  aws.String("toktik-videos"),
		Key:     aws.String(randomKey),
		Expires: &expireDate,
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

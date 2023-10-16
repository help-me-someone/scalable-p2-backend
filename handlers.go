package main

import (
	"bytes"
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
	tasks "github.com/help-me-someone/scalable-p2-tasks"
	"github.com/hibiken/asynq"
	"github.com/julienschmidt/httprouter"
)

// This API kickstarts the pipeline for saving
func HandleVideoSave(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
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
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

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

func GetUploadPresignedUrl(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
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

	username := r.Header.Get("X-Username")
	log.Println("User requesting presigned:", username)

	if len(username) == 0 {
		log.Println("Username not specified")
		w.WriteHeader(http.StatusBadRequest)
		resp := map[string]interface{}{
			"success": false,
			"message": "Error: username not specified.",
		}
		json.NewEncoder(w).Encode(resp)
		return
	}

	// I really need to enable cors
	w.Header().Set("Access-Control-Allow-Origin", "http://localhost:8000")
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
		return
	}

	// Create the client.
	client := s3.NewFromConfig(cfg)

	// Presign the client.
	presignClient := s3.NewPresignClient(client)

	// Create the random string we'll save the file to.
	randomKey := uniuri.NewLen(100)

	// We save it as "vid". The directory for the video is randomly generated.
	keyPath := fmt.Sprintf("users/%s/videos/%s/vid", username, randomKey)

	response, err := presignClient.PresignPutObject(context.TODO(), &s3.PutObjectInput{
		Bucket: aws.String("toktik-videos"),
		Key:    aws.String(keyPath),
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

func getVideoKey(username, videoname string) string {
	_ = username
	_ = videoname
	return "a8l0ohuRfbkIGY3rrv2vnE4L9yPolOxBC08r8PwYE8e5IyD6c6WUXLs59TX7aHWIXavh91ztlfRof3AbxWaZVR1P44UrHV7ucFvl"
}

//
// This function deals with retrieving data from digital ocean spaces.
//
func VideoHandler(w http.ResponseWriter, r *http.Request, p httprouter.Params) {
	username := r.Header.Get("X-Username")
	log.Println("User requesting video:", username)

	if len(username) == 0 {
		log.Println("Can't find username in X-Username")
		w.WriteHeader(http.StatusBadRequest)
		resp := map[string]interface{}{
			"success": false,
			"message": "Error: missing username header.",
		}
		json.NewEncoder(w).Encode(resp)
		return
	}

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
			"message": "failed to load aws config",
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
		return
	}

	// Create the client.
	client := s3.NewFromConfig(cfg)

	user := p.ByName("user")
	resource := p.ByName("video")
	keyPath := fmt.Sprintf("users/%s/videos/%s", user, getVideoKey(user, resource))

	log.Printf("Requesting for %s", keyPath)

	// Generate the HSL file.
	buf, err := GenerateHSLFile(keyPath, client)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		resp := map[string]interface{}{
			"success": false,
			"message": "failed to generate HLS file",
		}
		json.NewEncoder(w).Encode(resp)
		return
	}

	w.Header().Set("Access-Control-Allow-Origin", "http://localhost:8000")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
	w.Header().Set("Access-Control-Allow-Credentials", "true")

	http.ServeContent(w, r, "", time.Now(), bytes.NewReader((buf.Bytes())))
}

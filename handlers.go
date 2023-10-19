package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/dchest/uniuri"

	"github.com/help-me-someone/scalable-p2-db/models/video"
	tasks "github.com/help-me-someone/scalable-p2-tasks"
	"github.com/hibiken/asynq"
	"github.com/julienschmidt/httprouter"
)

func FailResponse(w http.ResponseWriter, status int, message string) {
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": false,
		"message": message,
	})
}

// This API kickstarts the pipeline for saving
func HandleVideoSave(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	// Ensure the method is correct.
	if r.Method != "POST" {
		log.Println("Error: Not GET request")
		FailResponse(w, http.StatusBadRequest, "Invalid method.")
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
		FailResponse(w, http.StatusInternalServerError, "Queue connection not specified.")
		return
	}
	queueConn, ok := qc.(*asynq.Client)
	if !ok {
		log.Println("Error: queue connection invalid type")
		FailResponse(w, http.StatusBadRequest, "Queue connection invalid type.")
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
		FailResponse(w, http.StatusBadRequest, "Failed to create task.")
		return
	}
	// TODO: ^^^ Clean this up, stop using headers...

	payload := struct {
		FileName string `json:"file_name"`
	}{}
	err = json.NewDecoder(r.Body).Decode(&payload)
	if err != nil {
		FailResponse(w, http.StatusBadRequest, "Failed to retrieve file name.")
		return
	}

	// Create the new entry.
	var buf bytes.Buffer
	json.NewEncoder(&buf).Encode(map[string]interface{}{
		"name":       payload.FileName,
		"key":        video_name, // TODO: Fix misleading name.
		"owner_name": user,
	})

	// Create a new entry on the database.
	resp, err := http.Post("http://db-svc:8083/video", "application/json", &buf)
	if err != nil || resp.StatusCode != http.StatusOK {
		log.Println(err)
		FailResponse(w, http.StatusInternalServerError, "Create user request failed.")
		return
	}
	defer resp.Body.Close()

	log.Println("Added the video to the database!")

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

	re := map[string]interface{}{
		"success": true,
		"message": "sucessfully enqueued task",
		"type":    info.Type,
		"user":    user,
		"video":   video_name, // This is the video address in the bucket.
	}
	json.NewEncoder(w).Encode(re)
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

	user := strings.ToLower(p.ByName("user"))
	resource := p.ByName("video")

	// Generate the HSL file.
	buf, err := GenerateHSLFile(client, user, resource)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		resp := map[string]interface{}{
			"success": false,
			"message": fmt.Sprintf("failed to generate HLS file: %s", err),
		}
		json.NewEncoder(w).Encode(resp)
		return
	}

	w.Header().Set("Access-Control-Allow-Origin", "http://localhost:8000")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
	w.Header().Set("Access-Control-Allow-Credentials", "true")

	http.ServeContent(w, r, "", time.Now(), bytes.NewReader((buf.Bytes())))
}

// Given a request, we return enough information for the frontend to be able to
// display it.
// NOTE: This does not return the HLS!
func HandleVideoInfo(w http.ResponseWriter, r *http.Request, p httprouter.Params) {
	// We only have the video name.
	videoName := p.ByName("video")
	username := p.ByName("user")
	if len(videoName) == 0 || len(username) == 0 {
		FailResponse(w, http.StatusBadRequest, "Video name/username not specified.")
		return
	}

	// Search for the entry.
	url := fmt.Sprintf("http://db-svc:8083/user/%s/videos/%s", username, videoName)
	resp, err := http.Get(url)
	if err != nil || resp.StatusCode != http.StatusOK {
		log.Println(err)
		FailResponse(w, http.StatusInternalServerError, "Create user request failed.")
		return
	}
	defer resp.Body.Close()

	vid := &video.Video{}
	if err := json.NewDecoder(resp.Body).Decode(vid); err != nil {
		log.Println("Could not decode video.")
		FailResponse(w, http.StatusInternalServerError, "Could not decode video.")
		return
	}

	// Create the presigned url for the thumbnail.
	thumbnailKey := fmt.Sprintf("users/%s/videos/%s/thumbnail", username, videoName)
	client, err := GetS3Client(region)
	if err != nil {
		log.Println("Could not create s3 client.")
		FailResponse(w, http.StatusInternalServerError, "Could not create s3 client.")
		return
	}

	log.Println("Thumbnail key:", thumbnailKey)

	url, err = GeneratePresignedUrl(thumbnailKey, client)
	if err != nil {
		log.Println("Failed to generate presigned url.")
		FailResponse(w, http.StatusInternalServerError, "Failed to generate presigned url.")
		return
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":   true,
		"message":   "Video found.",
		"video":     vid,
		"thumbnail": url,
	})
}

// The VideoFeedHandler handles when the frontend requests for content on the home page.
// The content is going to be used for the infinite scrolling on the frontend side.
// This will simply return a bunch of videos. With their thumbnail's url.
func VideoFeedHandler(w http.ResponseWriter, r *http.Request, p httprouter.Params) {

	// Attempt to get the query values.
	amountStr := p.ByName("amount")
	pageStr := p.ByName("page")
	if len(amountStr) == 0 || len(pageStr) == 0 {
		FailResponse(w, http.StatusBadRequest, "Failed to get videos.")
		return
	}

	// Query the database.
	url := fmt.Sprintf("http://db-svc:8083/popular/%s/%s", amountStr, pageStr)
	resp, err := http.Get(url)
	if err != nil {
		FailResponse(w, http.StatusInternalServerError, "Failed to request feed.")
	} else if resp.StatusCode != http.StatusOK {
		FailResponse(w, http.StatusBadRequest, "Failed to request feed.")
	}
	defer resp.Body.Close()

	// Decode the entries and get the key of each video.
	response := &struct {
		Success bool                       `json:"success"`
		Message string                     `json:"message"`
		Entries []video.VideoWithUserEntry `json:"videos"`
	}{}

	err = json.NewDecoder(resp.Body).Decode(&response)
	if err != nil {
		log.Println("Failed to decode:", err)
		FailResponse(w, http.StatusInternalServerError, "Failed to decode videos response.")
		return
	}

	// Create a new S3 client.
	client, err := GetS3Client(region)
	if err != nil {
		FailResponse(w, http.StatusInternalServerError, "Failed to get an S3 client.")
	}

	// Generate the response for the frontend.
	// For each video, we just generate the video thumbnail.
	type Entry struct {
		Video        video.VideoWithUserEntry `json:"video"`
		ThumbnailURL string                   `json:"thumbnail_url"`
	}
	entries := make([]Entry, 0)
	for _, v := range response.Entries {
		thumbnailUrl, err := GenerateVideoThumbnailUrl(client, v.Username, v.Key)
		if err != nil {
			log.Println("Something went wrong.")
			continue
		}
		entries = append(entries, Entry{
			Video:        v,
			ThumbnailURL: thumbnailUrl,
		})
	}

	// Send the response.
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "Successfully retrieved feed.",
		"entries": entries,
	})
}

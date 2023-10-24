package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/dchest/uniuri"

	"github.com/help-me-someone/scalable-p2-db/functions/crud"
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

	log.Println("Handling video save.")

	// Ensure the method is correct.
	if r.Method != "POST" {
		log.Println("Error: Not GET request")
		FailResponse(w, http.StatusBadRequest, "Invalid method.")
		return
	}

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

	log.Println("Decoding body.")

	payload := struct {
		FileName string `json:"file_name"`
	}{}
	err = json.NewDecoder(r.Body).Decode(&payload)
	if err != nil {
		FailResponse(w, http.StatusBadRequest, "Failed to retrieve file name.")
		return
	}

	// Add the new video entry to the database
	connection, _ := GetDatabaseConnection(DB_USERNAME, DB_PASSWORD, DB_IP)
	usr, _ := crud.GetUserByName(connection, user)
	_, err = crud.CreateVideo(
		connection,
		payload.FileName,
		video_name,
		usr.ID,
	)
	if err != nil {
		FailResponse(w, http.StatusInternalServerError, "Create user request failed.")
		return
	}

	// Queue the task.
	info, err := queueConn.Enqueue(t1)
	if err != nil {
		log.Println("Error: failed to queue task:", err)
		FailResponse(w, http.StatusBadRequest, "Failed to queue task.")
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
		FailResponse(w, http.StatusBadRequest, "Invalid method.")
		return
	}

	username := r.Header.Get("X-Username")
	log.Println("User requesting presigned:", username)

	if len(username) == 0 {
		log.Println("Username not specified")
		FailResponse(w, http.StatusBadRequest, "Username not specified.")
		return
	}

	w.Header().Set("Access-Control-Allow-Origin", "http://localhost:8000")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

	client, err := GetS3Client(region)
	if err != nil {
		FailResponse(w, http.StatusInternalServerError, "Failed to get s3 client.")
		return
	}

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
		FailResponse(w, http.StatusInternalServerError, "Error retrieving presigned object.")
		return
	}

	resp := map[string]interface{}{
		"success": true,
		"url":     response.URL,
		"key":     randomKey,
	}
	json.NewEncoder(w).Encode(resp)
}

//
// This function deals with retrieving data from digital ocean spaces.
//
func VideoHandler(w http.ResponseWriter, r *http.Request, p httprouter.Params) {

	client, err := GetS3Client(region)
	if err != nil {
		FailResponse(w, http.StatusInternalServerError, "Failed to get S3 client.")
		return
	}

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
	w.Header().Set("Access-Control-Allow-Headers", "*")
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
	connection, _ := GetDatabaseConnection(DB_USERNAME, DB_PASSWORD, DB_IP)
	vid, _ := crud.GetUserVideoFromUsername(connection, username, videoName)

	// Create the presigned url for the thumbnail.
	thumbnailKey := fmt.Sprintf("users/%s/videos/%s/thumbnail", username, videoName)
	client, err := GetS3Client(region)
	if err != nil {
		log.Println("Could not create s3 client.")
		FailResponse(w, http.StatusInternalServerError, "Could not create s3 client.")
		return
	}

	url, err := GeneratePresignedUrl(thumbnailKey, client)
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

// Corresponds to the "/watch/:user/:video" URL.
// This handler is exactly the same as the one for video info,
// however this also increase the view count of the video.
func HandleVideoWatchInfo(w http.ResponseWriter, r *http.Request, p httprouter.Params) {
	// We only have the video name.
	videoName := p.ByName("video")
	username := p.ByName("user")
	if len(videoName) == 0 || len(username) == 0 {
		FailResponse(w, http.StatusBadRequest, "Video name/username not specified.")
		return
	}

	// Search for the entry.
	connection, _ := GetDatabaseConnection(DB_USERNAME, DB_PASSWORD, DB_IP)
	vid, _ := crud.GetUserVideoFromUsername(connection, username, videoName)

	// Create the presigned url for the thumbnail.
	thumbnailKey := fmt.Sprintf("users/%s/videos/%s/thumbnail", username, videoName)
	client, err := GetS3Client(region)
	if err != nil {
		log.Println("Could not create s3 client.")
		FailResponse(w, http.StatusInternalServerError, "Could not create s3 client.")
		return
	}

	url, err := GeneratePresignedUrl(thumbnailKey, client)
	if err != nil {
		log.Println("Failed to generate presigned url.")
		FailResponse(w, http.StatusInternalServerError, "Failed to generate presigned url.")
		return
	}

	// Increment the view count of the video.
	err = crud.UpdateVideoViewIncrement(connection, vid.ID)
	if err != nil {
		log.Println("Failed to increment video view count.")
		FailResponse(w, http.StatusBadRequest, "Failed to increment video view count")
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

	// Convert query into numerical values.
	amount, err := strconv.Atoi(amountStr)
	page, err := strconv.Atoi(pageStr)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": "Failed to get videos.",
		})
		return
	}

	// Create a new connection

	connection, _ := GetDatabaseConnection(DB_USERNAME, DB_PASSWORD, DB_IP)
	vids, err := crud.GetTopPopularVideos(connection, page, amount)

	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": "Failed to get videos.",
		})
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
	for _, v := range vids {
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

func GetVideoByRank(w http.ResponseWriter, r *http.Request, p httprouter.Params) {

	log.Println("Getting video by rank.")

	// Attempt to get the query values.
	rankStr := p.ByName("rank")
	if len(rankStr) == 0 {
		FailResponse(w, http.StatusBadRequest, "Failed to get videos.")
		return
	}

	// Get connection.
	connection, _ := GetDatabaseConnection(DB_USERNAME, DB_PASSWORD, DB_IP)

	// Query the database.
	rank, _ := strconv.Atoi(rankStr)
	vid, err := crud.GetVideoByRank(connection, rank)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": "Video not found.",
		})
		return
	}

	// Create a new S3 client.
	client, err := GetS3Client(region)
	if err != nil {
		FailResponse(w, http.StatusInternalServerError, "Failed to get an S3 client.")
		log.Panicln("Failed to generate S3 client.")
		return
	}

	// Generate the response for the frontend.
	// For each video, we just generate the video thumbnail.
	thumbnailUrl, err := GenerateVideoThumbnailUrl(client, vid.Username, vid.Key)
	if err != nil {
		log.Println("Failed to generate thumbnail.")
		return
	}

	type Entry struct {
		Video        video.VideoWithUserEntry `json:"video"`
		ThumbnailURL string                   `json:"thumbnail_url"`
	}
	entry := &Entry{
		Video:        *vid,
		ThumbnailURL: thumbnailUrl,
	}

	// Send the response.
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "Successfully retrieved feed.",
		"entry":   entry,
	})
}

func GetUserVideos(w http.ResponseWriter, r *http.Request, p httprouter.Params) {
	// Make a new database client
	connection, err := GetDatabaseConnection(DB_USERNAME, DB_PASSWORD, DB_IP)
	if err != nil {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": "Failed to get the database connection.",
		})
		return
	}

	// Retrieve the username
	username := p.ByName("user")
	if len(username) == 0 {
		log.Println("Where the username be at??")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": "No username specified",
		})
		return
	}

	// Retrieve the user's vidoes.
	videos, err := crud.GetUserVideosFromUsername(connection, username)
	if err != nil {
		log.Println("Something bad has truly happened.")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": "Failed to get user's videos.",
			"videos":  videos,
		})
		return
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "Successfully retrieved user videos.",
		"videos":  videos,
	})

}

package main

import (
	"context"
	"log"
	"net/http"
	"os"

	db "github.com/help-me-someone/scalable-p2-db"
	"github.com/help-me-someone/scalable-p2-db/models/user"
	"github.com/help-me-someone/scalable-p2-db/models/video"
	"github.com/hibiken/asynq"
	"github.com/julienschmidt/httprouter"
	"github.com/rs/cors"
)

const (
	region = "sgp1"
)

var (
	ALLOWED_ORIGIN string
	DB_USERNAME    string
	DB_PASSWORD    string
	DB_IP          string
	MODE           string
)

func loadEnvs() {
	ALLOWED_ORIGIN = os.Getenv("ALLOWED_ORIGIN")
	DB_USERNAME = os.Getenv("DB_USERNAME")
	DB_PASSWORD = os.Getenv("DB_PASSWORD")
	DB_IP = os.Getenv("DB_IP")
	MODE = os.Getenv("MODE")
}

func main() {
	// Retrieve all environment variables.
	loadEnvs()

	// Initalize the database.
	toktik_db, _ := GetDatabaseConnection(DB_USERNAME, DB_PASSWORD, DB_IP)
	if !toktik_db.Migrator().HasTable(&user.User{}) && !toktik_db.Migrator().HasTable(&video.Video{}) {
		db.InitTables(toktik_db)
		log.Println("Database initialized!")
	}

	taskQueueHandler := &TaskQueueHandler{
		Connection: asynq.NewClient(asynq.RedisClientOpt{
			Addr: "redis:6379",
		}),
	}

	mux := httprouter.New()
	mux.GET("/upload", GetUploadPresignedUrl)
	mux.POST("/save", taskQueueHandler.TaskMiddleware(HandleVideoSave))

	// The following endpoint uses database:
	mux.GET("/users/:user/videos/:video", VideoHandler)

	// Retrieve enough information for the frontend to be able to render.
	mux.GET("/users/:user/videos/:video/info", HandleVideoInfo)
	mux.GET("/watch/:user/:video/info", HandleVideoWatchInfo)
	mux.GET("/video/feed/:amount/:page", VideoFeedHandler)
	mux.GET("/video/rank/:rank", GetVideoByRank)
	mux.GET("/users/:user/videos", GetUserVideos)

	handler := cors.New(cors.Options{
		AllowedOrigins:   []string{ALLOWED_ORIGIN},
		AllowCredentials: true,
		AllowedHeaders: []string{
			"Hx-Current-Url",
			"Hx-Request",
			"Hx-Target",
			"Hx-Boosted",
			"Hx-Current-Url",
			"Hx-Request",
			"Hx-Trigger",
			"Content-Type",
			"X-Custom-Header",
			"*",
		},
		AllowedMethods: []string{
			"POST",
			"GET",
			"PUT",
			"OPTIONS",
			"*",
		},

		// Enable Debugging for testing, consider disabling in production
		Debug: (MODE == "DEBUG"),
	}).Handler(mux)

	log.Println("Server started successfully, listening on port 7000.")
	log.Fatal(http.ListenAndServe(":7000", handler))
}

type TaskQueueHandler struct {
	Connection *asynq.Client
}

func (t *TaskQueueHandler) TaskMiddleware(next func(http.ResponseWriter, *http.Request, httprouter.Params)) func(http.ResponseWriter, *http.Request, httprouter.Params) {
	return func(w http.ResponseWriter, r *http.Request, p httprouter.Params) {
		ctx := context.WithValue(r.Context(), "queue_conn", t.Connection)
		next(w, r.WithContext(ctx), p)
	}
}

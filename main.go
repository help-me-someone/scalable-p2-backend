package main

import (
	"context"
	"log"
	"net/http"

	"github.com/hibiken/asynq"
	"github.com/julienschmidt/httprouter"
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

	mux := httprouter.New()
	mux.GET("/upload", GetUploadPresignedUrl)
	mux.POST("/save", taskQueueHandler.TaskMiddleware(HandleVideoSave))

	// The following endpoint uses database:
	mux.GET("/users/:user/videos/:video", VideoHandler)

	// Retrieve enough information for the frontend to be able to render.
	mux.GET("/users/:user/videos/:video/info", HandleVideoInfo)
	mux.GET("/video/feed/:amount/:page", VideoFeedHandler)
	mux.GET("/video/rank/:rank", GetVideoByRank)

	log.Println("Server started successfully, listening on port 7000.")
	log.Fatal(http.ListenAndServe(":7000", mux))
}

// TODO: Clean this up maybe.
func enableCors(w *http.ResponseWriter) {
	(*w).Header().Set("Access-Control-Allow-Origin", "*")
	(*w).Header().Set("Access-Control-Allow-Headers", "Content-Type")
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

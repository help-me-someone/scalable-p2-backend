package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
)

func main() {
	http.HandleFunc("/", upload)
	log.Println("Server started successfully, listening on port 7000.")
	log.Fatal(http.ListenAndServe(":7000", nil))
}

func upload(w http.ResponseWriter, r *http.Request) {
	if r.Method == "POST" || r.Method == "OPTIONS" {

		username := r.Header.Get("username")

		// No username...
		if len(username) == 0 {
			log.Println("Username is not found in request headers.")
			w.WriteHeader(http.StatusUnauthorized)
			resp := map[string]interface{}{
				"success": false,
				"message": "user not specified",
			}
			json.NewEncoder(w).Encode(resp)
			return
		}

		svc := NewVideoUploadService()

		// Limit the size to 32mb.
		r.Body = http.MaxBytesReader(w, r.Body, 32<<20+512)

		// Parse the file data.
		err := r.ParseMultipartForm(32 << 20) // 32Mb
		if err != nil {
			log.Println(err.Error())
			log.Println("Can't parse multipart form.")
			http.Error(w, err.Error(), http.StatusBadRequest)

			resp := map[string]interface{}{
				"success": false,
				"message": "can't parse multipart form",
			}
			json.NewEncoder(w).Encode(resp)
			return
		}

		file_name := r.FormValue("file_name")

		// Get the file from the request.
		file, file_headers, err := r.FormFile("file")
		if err != nil {
			log.Println("File not found.")
			log.Println(err.Error())
			http.Error(w, err.Error(), http.StatusBadRequest)
			resp := map[string]interface{}{
				"success": false,
				"message": "file not found",
			}
			json.NewEncoder(w).Encode(resp)
			return
		}
		defer file.Close()

		// Create the context for our service.
		ctx := context.Background()
		ctx = context.WithValue(ctx, "file", file)
		ctx = context.WithValue(ctx, "file_name", file_name)
		ctx = context.WithValue(ctx, "username", username)
		ctx = context.WithValue(ctx, "file_header", file_headers)

		// Perform the service.
		_, err = svc.Action(ctx)

		if err != nil {
			log.Println("Action failed.")
			log.Println(err.Error())
			http.Error(w, err.Error(), http.StatusBadRequest)

			resp := map[string]interface{}{
				"success": false,
				"message": err.Error(),
			}
			json.NewEncoder(w).Encode(resp)
			return
		}

		resp := map[string]interface{}{
			"success": true,
			"message": "successfully uploaded",
		}
		json.NewEncoder(w).Encode(resp)

		// Let's just log, I don't know what is going on.
		// log.Println(msg)
		return
	}
	log.Println("Incorrect method type.")

	w.WriteHeader(http.StatusBadRequest)
	resp := map[string]interface{}{
		"success": false,
		"message": "incorrect method type",
	}
	json.NewEncoder(w).Encode(resp)
}

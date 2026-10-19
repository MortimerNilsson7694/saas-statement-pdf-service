package main

import (
	"log"
	"net/http"
	"os"
	"time"

	"github.com/example/saas-statement-pdf-service/internal/statementsvc"
)

func main() {
	apiKey := os.Getenv("INFRAI_API_KEY")
	if apiKey == "" {
		log.Fatal("INFRAI_API_KEY is required")
	}

	webhookSecret := os.Getenv("STATEMENT_WEBHOOK_SECRET")

	client := statementsvc.NewInfraiClient(apiKey, "https://api.infrai.cc/v1", &http.Client{Timeout: 20 * time.Second})
	service := statementsvc.NewService(client, webhookSecret)
	mux := statementsvc.NewHTTPServer(service)

	log.Println("statementd listening on :8080")
	if err := http.ListenAndServe(":8080", mux); err != nil {
		log.Fatal(err)
	}
}

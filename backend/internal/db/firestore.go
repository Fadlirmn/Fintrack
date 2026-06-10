package db

import (
	"context"
	"log"

	firestore "cloud.google.com/go/firestore"
	firebase "firebase.google.com/go"
)

// InitFirestore initializes the Firebase App and returns a Firestore client.
// It relies on GOOGLE_APPLICATION_CREDENTIALS environment variable pointing to the JSON secret key.
func InitFirestore(ctx context.Context, projectID string) (*firestore.Client, error) {
	config := &firebase.Config{ProjectID: projectID}
	
	app, err := firebase.NewApp(ctx, config)
	if err != nil {
		log.Printf("Failed to initialize Firebase App: %v\n", err)
		return nil, err
	}

	client, err := app.Firestore(ctx)
	if err != nil {
		log.Printf("Failed to create Firestore client: %v\n", err)
		return nil, err
	}

	log.Println("Successfully established connection to Firebase Firestore")
	return client, nil
}

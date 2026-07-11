// Package db wraps the Firestore client with helper methods.
// When FIRESTORE_EMULATOR_HOST is set, the client automatically connects to the local emulator.
package db

import (
	"context"
	"fmt"
	"os"

	firebase "firebase.google.com/go/v4"
	"cloud.google.com/go/firestore"
	"google.golang.org/api/option"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Client holds a Firestore client and the Firebase project ID.
// App is the shared Firebase app in production (nil when connected to the
// local emulator, which needs no app).
type Client struct {
	FS        *firestore.Client
	App       *firebase.App
	ProjectID string
}

// NewFirebaseApp builds a Firebase app for the configured project, using
// GOOGLE_APPLICATION_CREDENTIALS when set and Application Default
// Credentials otherwise. Shared by Firestore (production) and Auth so the
// env-var/credential wiring lives in exactly one place.
func NewFirebaseApp(ctx context.Context) (*firebase.App, error) {
	projectID := os.Getenv("FIREBASE_PROJECT_ID")
	if projectID == "" {
		return nil, fmt.Errorf("FIREBASE_PROJECT_ID env var not set")
	}
	conf := &firebase.Config{ProjectID: projectID}
	if credFile := os.Getenv("GOOGLE_APPLICATION_CREDENTIALS"); credFile != "" {
		return firebase.NewApp(ctx, conf, option.WithCredentialsFile(credFile))
	}
	return firebase.NewApp(ctx, conf)
}

// New initialises a Firestore client.
// Respects FIRESTORE_EMULATOR_HOST for local development.
// On Cloud Run uses Application Default Credentials automatically.
func New(ctx context.Context) (*Client, error) {
	projectID := os.Getenv("FIREBASE_PROJECT_ID")
	if projectID == "" {
		return nil, fmt.Errorf("FIREBASE_PROJECT_ID env var not set")
	}

	// When the emulator is configured, create the client directly without
	// credentials so gRPC connects in plain-text to the local emulator.
	if emHost := os.Getenv("FIRESTORE_EMULATOR_HOST"); emHost != "" {
		fmt.Printf("[db] connecting to Firestore emulator at %s for project %s\n", emHost, projectID)
		fs, err := firestore.NewClient(ctx, projectID)
		if err != nil {
			return nil, fmt.Errorf("initialise firestore emulator client: %w", err)
		}
		fmt.Println("[db] Firestore emulator client created successfully")
		return &Client{FS: fs, ProjectID: projectID}, nil
	}

	// Production: use Firebase Admin SDK with credentials.
	app, err := NewFirebaseApp(ctx)
	if err != nil {
		return nil, fmt.Errorf("initialise firebase app: %w", err)
	}

	fs, err := app.Firestore(ctx)
	if err != nil {
		return nil, fmt.Errorf("initialise firestore client: %w", err)
	}

	return &Client{FS: fs, App: app, ProjectID: projectID}, nil
}

// Close releases the underlying Firestore connection.
func (c *Client) Close() error {
	return c.FS.Close()
}

// GetDoc reads a single document into out (pointer to struct).
// Returns (false, nil) when the document does not exist.
func (c *Client) GetDoc(ctx context.Context, collection, id string, out interface{}) (bool, error) {
	snap, err := c.FS.Collection(collection).Doc(id).Get(ctx)
	if err != nil {
		if status.Code(err) == codes.NotFound {
			return false, nil
		}
		return false, fmt.Errorf("get %s/%s: %w", collection, id, err)
	}
	if err := snap.DataTo(out); err != nil {
		return false, fmt.Errorf("decode %s/%s: %w", collection, id, err)
	}
	return true, nil
}

// SetDoc creates or overwrites a document.
func (c *Client) SetDoc(ctx context.Context, collection, id string, data interface{}) error {
	if _, err := c.FS.Collection(collection).Doc(id).Set(ctx, data); err != nil {
		return fmt.Errorf("set %s/%s: %w", collection, id, err)
	}
	return nil
}

// UpdateDoc applies field-level updates to an existing document.
func (c *Client) UpdateDoc(ctx context.Context, collection, id string, updates []firestore.Update) error {
	if _, err := c.FS.Collection(collection).Doc(id).Update(ctx, updates); err != nil {
		return fmt.Errorf("update %s/%s: %w", collection, id, err)
	}
	return nil
}

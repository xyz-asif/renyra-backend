// Command backfill-repost-publishedat repairs reposts that were created before
// ToggleRepost started stamping PublishedAt (commit e56480a).
//
// Background: the home feed sorts by publishedAt DESC with no createdAt fallback
// (introduced in 4e1a3e1). Reposts created before the fix have no publishedAt, so
// they sort to the very bottom of the feed and never surface — the "@user reposted"
// card silently disappears for the reposter and their followers.
//
// This backfill sets publishedAt = createdAt for every repost missing publishedAt,
// which places each repost back at its original position in the feed.
//
// Usage:
//
//	go run ./cmd/backfill-repost-publishedat            # apply the fix
//	go run ./cmd/backfill-repost-publishedat -dry-run   # count affected docs, change nothing
package main

import (
	"context"
	"flag"
	"log"
	"time"

	"github.com/xyz-asif/renyra-backend/internal/config"
	"github.com/xyz-asif/renyra-backend/internal/database"
	"go.mongodb.org/mongo-driver/v2/bson"
)

func main() {
	dryRun := flag.Bool("dry-run", false, "count affected reposts without modifying anything")
	flag.Parse()

	cfg, err := config.LoadConfig()
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	db, err := database.Connect(cfg.MongoDBURI, cfg.DatabaseName)
	if err != nil {
		log.Fatalf("db connect: %v", err)
	}
	defer db.Client.Disconnect(context.Background())

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	poems := db.Database.Collection("poems")

	// Reposts (isRepost:true) that have no publishedAt. $exists:false catches
	// documents missing the field entirely; the null branch catches any that
	// were explicitly stored as null.
	filter := bson.M{
		"isRepost": true,
		"$or": []bson.M{
			{"publishedAt": bson.M{"$exists": false}},
			{"publishedAt": nil},
		},
	}

	affected, err := poems.CountDocuments(ctx, filter)
	if err != nil {
		log.Fatalf("count: %v", err)
	}
	log.Printf("reposts missing publishedAt: %d", affected)

	if *dryRun {
		log.Println("dry-run: no changes made")
		return
	}
	if affected == 0 {
		log.Println("nothing to backfill")
		return
	}

	// Pipeline update: set publishedAt = createdAt. Using an aggregation-pipeline
	// update lets us reference the document's own createdAt field.
	res, err := poems.UpdateMany(ctx, filter, bson.A{
		bson.M{"$set": bson.M{"publishedAt": "$createdAt"}},
	})
	if err != nil {
		log.Fatalf("update: %v", err)
	}
	log.Printf("backfill complete: matched=%d modified=%d", res.MatchedCount, res.ModifiedCount)
}

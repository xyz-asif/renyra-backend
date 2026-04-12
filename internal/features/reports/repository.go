package reports

import (
	"context"
	"time"

	"github.com/xyz-asif/renyra-backend/internal/models"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

type Repository interface {
	Create(ctx context.Context, report *models.Report) error
	GetAll(ctx context.Context, isBug *bool, limit, offset int) ([]models.Report, bool, error)
	GetByUserID(ctx context.Context, userID bson.ObjectID, isBug *bool, limit, offset int) ([]models.Report, bool, error)
	GetByID(ctx context.Context, id bson.ObjectID) (*models.Report, error)
	Update(ctx context.Context, id bson.ObjectID, updates map[string]interface{}) error
}

type repository struct {
	collection *mongo.Collection
}

func NewRepository(db *mongo.Database) Repository {
	return &repository{
		collection: db.Collection("reports"),
	}
}

func (r *repository) Create(ctx context.Context, report *models.Report) error {
	report.CreatedAt = time.Now()
	report.UpdatedAt = time.Now()
	
	res, err := r.collection.InsertOne(ctx, report)
	if err != nil {
		return err
	}
	report.ID = res.InsertedID.(bson.ObjectID)
	return nil
}

func (r *repository) GetAll(ctx context.Context, isBug *bool, limit, offset int) ([]models.Report, bool, error) {
	filter := bson.M{}
	if isBug != nil {
		filter["isBug"] = *isBug
	}

	importOptions := options.Find().
		SetLimit(int64(limit + 1)). // fetch one extra to check if hasMore
		SetSkip(int64(offset)).
		SetSort(bson.D{{Key: "createdAt", Value: -1}}) // default sort newest first

	cursor, err := r.collection.Find(ctx, filter, importOptions)
	if err != nil {
		return nil, false, err
	}
	defer cursor.Close(ctx)

	var reports []models.Report
	if err := cursor.All(ctx, &reports); err != nil {
		return nil, false, err
	}

	hasMore := false
	if len(reports) > limit {
		hasMore = true
		reports = reports[:limit]
	}

	return reports, hasMore, nil
}

func (r *repository) GetByUserID(ctx context.Context, userID bson.ObjectID, isBug *bool, limit, offset int) ([]models.Report, bool, error) {
	filter := bson.M{"userId": userID}
	if isBug != nil {
		filter["isBug"] = *isBug
	}

	importOptions := options.Find().
		SetLimit(int64(limit + 1)).
		SetSkip(int64(offset)).
		SetSort(bson.D{{Key: "createdAt", Value: -1}})

	cursor, err := r.collection.Find(ctx, filter, importOptions)
	if err != nil {
		return nil, false, err
	}
	defer cursor.Close(ctx)

	var reports []models.Report
	if err := cursor.All(ctx, &reports); err != nil {
		return nil, false, err
	}

	hasMore := false
	if len(reports) > limit {
		hasMore = true
		reports = reports[:limit]
	}

	return reports, hasMore, nil
}

func (r *repository) GetByID(ctx context.Context, id bson.ObjectID) (*models.Report, error) {
	var report models.Report
	err := r.collection.FindOne(ctx, bson.M{"_id": id}).Decode(&report)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, nil // Return nil if not found
		}
		return nil, err
	}
	return &report, nil
}

func (r *repository) Update(ctx context.Context, id bson.ObjectID, updates map[string]interface{}) error {
	updates["updatedAt"] = time.Now()
	_, err := r.collection.UpdateOne(ctx, bson.M{"_id": id}, bson.M{"$set": updates})
	return err
}

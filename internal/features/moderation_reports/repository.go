package moderation_reports

import (
	"context"
	"time"

	"github.com/xyz-asif/renyra-backend/internal/models"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

type Repository interface {
	Create(ctx context.Context, report *models.ModerationReport) error
	GetAll(ctx context.Context, targetType *models.ReportTargetType, status *string, limit, offset int) ([]models.ModerationReport, bool, error)
	GetByReporterID(ctx context.Context, reporterID bson.ObjectID, limit, offset int) ([]models.ModerationReport, bool, error)
	GetByID(ctx context.Context, id bson.ObjectID) (*models.ModerationReport, error)
	Update(ctx context.Context, id bson.ObjectID, updates map[string]interface{}) error
	GetByReporterAndTarget(ctx context.Context, reporterID, targetID bson.ObjectID, targetType models.ReportTargetType) (*models.ModerationReport, error)
}

type repository struct {
	collection *mongo.Collection
}

func NewRepository(db *mongo.Database) Repository {
	return &repository{
		collection: db.Collection("moderation_reports"),
	}
}

func (r *repository) Create(ctx context.Context, report *models.ModerationReport) error {
	report.CreatedAt = time.Now()
	report.UpdatedAt = time.Now()
	
	res, err := r.collection.InsertOne(ctx, report)
	if err != nil {
		return err
	}
	report.ID = res.InsertedID.(bson.ObjectID)
	return nil
}

func (r *repository) GetAll(ctx context.Context, targetType *models.ReportTargetType, status *string, limit, offset int) ([]models.ModerationReport, bool, error) {
	filter := bson.M{}
	if targetType != nil {
		filter["targetType"] = *targetType
	}
	if status != nil {
		filter["status"] = *status
	}

	opts := options.Find().
		SetLimit(int64(limit + 1)).
		SetSkip(int64(offset)).
		SetSort(bson.D{{Key: "createdAt", Value: -1}})

	cursor, err := r.collection.Find(ctx, filter, opts)
	if err != nil {
		return nil, false, err
	}
	defer cursor.Close(ctx)

	var reports []models.ModerationReport
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

func (r *repository) GetByReporterID(ctx context.Context, reporterID bson.ObjectID, limit, offset int) ([]models.ModerationReport, bool, error) {
	filter := bson.M{"reporterId": reporterID}

	opts := options.Find().
		SetLimit(int64(limit + 1)).
		SetSkip(int64(offset)).
		SetSort(bson.D{{Key: "createdAt", Value: -1}})

	cursor, err := r.collection.Find(ctx, filter, opts)
	if err != nil {
		return nil, false, err
	}
	defer cursor.Close(ctx)

	var reports []models.ModerationReport
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

func (r *repository) GetByID(ctx context.Context, id bson.ObjectID) (*models.ModerationReport, error) {
	var report models.ModerationReport
	err := r.collection.FindOne(ctx, bson.M{"_id": id}).Decode(&report)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, nil
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

func (r *repository) GetByReporterAndTarget(ctx context.Context, reporterID, targetID bson.ObjectID, targetType models.ReportTargetType) (*models.ModerationReport, error) {
	filter := bson.M{
		"reporterId": reporterID,
		"targetId":   targetID,
		"targetType": targetType,
	}
	var report models.ModerationReport
	err := r.collection.FindOne(ctx, filter).Decode(&report)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, nil
		}
		return nil, err
	}
	return &report, nil
}

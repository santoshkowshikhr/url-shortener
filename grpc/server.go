package grpcserver

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"go-api-server/api"
	"go-api-server/model"
	"log"
	"regexp"
	"strings"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type Server struct {
	api.UnimplementedShortenerServiceServer
}

func generateShortCode(n int) string {
	var shortCode string
	b := make([]byte, n)

	rand.Read(b)

	shortCode = base64.URLEncoding.EncodeToString(b)[:n]

	return shortCode
}

func (s *Server) CreateShortener(ctx context.Context, req *api.ShortenerRequest) (*api.ShortenerResponse, error) {
	finalCode := req.CustomAlias

	if finalCode == "" {
		finalCode = generateShortCode(6)
	}

	if !isValidAlias(finalCode) {
		return nil, status.Errorf(codes.InvalidArgument, "custom_alias must be alphanumeric")
	}

	var expireAt *time.Time
	if req.ExpireInDays > 0 {
		t := time.Now().Add(time.Duration(req.ExpireInDays) * 24 * time.Hour)
		expireAt = &t
	}
	err := model.InsertURLMapping(req.LongUrl, finalCode, req.CustomAlias, expireAt)
	if err != nil {
		if isDuplicateError(err) {
			return nil, status.Errorf(codes.AlreadyExists, "alias already in use")
		}
	}
	shortURL := "http://localhost:8080/" + finalCode
	return &api.ShortenerResponse{ShortUrl: shortURL}, nil
}

func (s *Server) RedirectShortener(ctx context.Context, req *api.RedirectRequest) (*api.RedirectResponse, error) {
	// Check Redis cache first
	if model.RDB == nil {
		log.Println("Redis client not initialized")
	}

	key := fmt.Sprintf("short:%s", req.ShortUrl)
	longURL, err := model.RDB.Get(ctx, key).Result()
	if err == nil {
		return &api.RedirectResponse{LongUrl: longURL}, nil
	}

	// If not found in cache → fallback to DB
	longURL, expireAt, err := model.GetLongURLWithExpiry(req.ShortUrl)
	if err != nil {
		return nil, status.Errorf(codes.NotFound, "short code not found")
	}

	if expireAt != nil && time.Now().After(*expireAt) {
		return nil, status.Errorf(codes.NotFound, "link has expired")
	}

	// Cache the result with TTL
	var ttl time.Duration = 0
	if expireAt != nil {
		ttl = time.Until(*expireAt)
	}

	err = model.RDB.Set(ctx, key, longURL, ttl).Err()
	if err != nil {
		log.Printf("Redis cache set failed: %v", err)
	}

	return &api.RedirectResponse{LongUrl: longURL}, nil
}

func isValidAlias(alias string) bool {
	match, _ := regexp.MatchString(`^[a-zA-Z0-9_-]{3,20}$`, alias)
	return match
}

func isDuplicateError(err error) bool {
	return err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed")
}

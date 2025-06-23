package grpcserver_test

import (
	"context"
	"go-api-server/api"
	grpcserver "go-api-server/grpc"
	"go-api-server/model"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func init() {
	model.InitDB("file::memory:?cache=shared") // You can define InitDB for testing
	model.InitRedis()                          // Use a running Redis or mock
}

func TestCreateShortURL_Basic(t *testing.T) {
	server := &grpcserver.Server{}
	resp, err := server.CreateShortener(context.Background(), &api.ShortenerRequest{
		LongUrl:      "https://www.educative.io/courses/grokking-the-system-design-interview",
		CustomAlias:  "testalias",
		ExpireInDays: 3,
	})

	assert.NoError(t, err)
	assert.Contains(t, resp.ShortUrl, "testalias")
}

func TestRedirectShortener_Valid(t *testing.T) {
	server := &grpcserver.Server{}

	// Insert directly to simulate existing record
	expireAt := time.Now().Add(24 * time.Hour)
	model.InsertURLMapping("https://example.com/redirect", "redir123", "redir123", &expireAt)

	resp, err := server.RedirectShortener(context.Background(), &api.RedirectRequest{
		ShortUrl: "redir123",
	})

	assert.NoError(t, err)
	assert.Equal(t, "https://example.com/redirect", resp.LongUrl)
}

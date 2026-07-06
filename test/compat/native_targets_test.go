package compat_test

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/redis/go-redis/v9"
)

func TestDynamoDBSDKPutGetItemAgainstAlternatorEndpoint(t *testing.T) {
	items := map[string]map[string]types.AttributeValue{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Header.Get("X-Amz-Target") {
		case "DynamoDB_20120810.PutItem":
			var req struct {
				Item map[string]map[string]string `json:"Item"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatal(err)
			}
			items[req.Item["id"]["S"]] = map[string]types.AttributeValue{
				"id":    &types.AttributeValueMemberS{Value: req.Item["id"]["S"]},
				"value": &types.AttributeValueMemberS{Value: req.Item["value"]["S"]},
			}
			_, _ = w.Write([]byte(`{}`))
		case "DynamoDB_20120810.GetItem":
			var req struct {
				Key map[string]map[string]string `json:"Key"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatal(err)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"Item": map[string]any{
					"id":    map[string]string{"S": req.Key["id"]["S"]},
					"value": map[string]string{"S": items[req.Key["id"]["S"]]["value"].(*types.AttributeValueMemberS).Value},
				},
			})
		default:
			http.Error(w, "unsupported target", http.StatusBadRequest)
		}
	}))
	defer server.Close()

	client := dynamodb.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *dynamodb.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	_, err := client.PutItem(context.Background(), &dynamodb.PutItemInput{
		TableName: aws.String("things"),
		Item: map[string]types.AttributeValue{
			"id":    &types.AttributeValueMemberS{Value: "1"},
			"value": &types.AttributeValueMemberS{Value: "hello"},
		},
	})
	if err != nil {
		t.Fatalf("PutItem() error = %v", err)
	}

	got, err := client.GetItem(context.Background(), &dynamodb.GetItemInput{
		TableName: aws.String("things"),
		Key: map[string]types.AttributeValue{
			"id": &types.AttributeValueMemberS{Value: "1"},
		},
	})
	if err != nil {
		t.Fatalf("GetItem() error = %v", err)
	}
	if got.Item["value"].(*types.AttributeValueMemberS).Value != "hello" {
		t.Fatalf("GetItem value = %#v, want hello", got.Item["value"])
	}
}

func TestRedisClientSetGetAgainstNativeProtocol(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = listener.Close() }()

	done := make(chan struct{})
	go serveTinyRedis(t, listener, done)

	client := redis.NewClient(&redis.Options{Addr: listener.Addr().String()})
	defer func() { _ = client.Close() }()

	ctx := context.Background()
	if err := client.Set(ctx, "hello", "world", 0).Err(); err != nil {
		t.Fatalf("Set() error = %v", err)
	}
	if got := client.Get(ctx, "hello").Val(); got != "world" {
		t.Fatalf("Get() = %q, want world", got)
	}
	<-done
}

func serveTinyRedis(t *testing.T, listener net.Listener, done chan<- struct{}) {
	t.Helper()
	defer close(done)

	conn, err := listener.Accept()
	if err != nil {
		t.Error(err)
		return
	}
	defer func() { _ = conn.Close() }()

	values := map[string]string{}
	reader := bufio.NewReader(conn)
	for {
		parts, err := readRESPArray(reader)
		if err != nil {
			return
		}
		switch strings.ToUpper(parts[0]) {
		case "HELLO":
			_, _ = conn.Write([]byte("%0\r\n"))
		case "CLIENT":
			_, _ = conn.Write([]byte("+OK\r\n"))
		case "SET":
			values[parts[1]] = parts[2]
			_, _ = conn.Write([]byte("+OK\r\n"))
		case "GET":
			value := values[parts[1]]
			_, _ = fmt.Fprintf(conn, "$%d\r\n%s\r\n", len(value), value)
			return
		default:
			_, _ = conn.Write([]byte("-ERR unsupported\r\n"))
		}
	}
}

func readRESPArray(reader *bufio.Reader) ([]string, error) {
	line, err := reader.ReadString('\n')
	if err != nil {
		return nil, err
	}
	var count int
	if _, err := fmt.Sscanf(line, "*%d\r\n", &count); err != nil {
		return nil, err
	}
	parts := make([]string, 0, count)
	for i := 0; i < count; i++ {
		var length int
		line, err = reader.ReadString('\n')
		if err != nil {
			return nil, err
		}
		if _, err := fmt.Sscanf(line, "$%d\r\n", &length); err != nil {
			return nil, err
		}
		buf := make([]byte, length+2)
		if _, err := reader.Read(buf); err != nil {
			return nil, err
		}
		parts = append(parts, string(buf[:length]))
	}
	return parts, nil
}

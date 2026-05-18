package aliyun_test

import (
	"context"
	"testing"
	"time"

	"ecs-controller/internal/aliyun"
)

func TestBuildRPCRequestIncludesSignatureAndAction(t *testing.T) {
	client := aliyun.NewClient("ak", "sk", aliyun.SiteChina, 5*time.Second)

	req, err := client.BuildRPCRequest(context.Background(), aliyun.RPCRequest{
		Host:    "ecs.cn-hangzhou.aliyuncs.com",
		Version: "2014-05-26",
		Action:  "DescribeInstances",
		Query: map[string]string{
			"RegionId": "cn-hangzhou",
		},
	})
	if err != nil {
		t.Fatalf("BuildRPCRequest() error = %v", err)
	}

	query := req.URL.Query()
	if query.Get("Action") != "DescribeInstances" {
		t.Fatalf("Action = %q", query.Get("Action"))
	}
	if query.Get("SignatureMethod") != "HMAC-SHA1" {
		t.Fatalf("SignatureMethod = %q", query.Get("SignatureMethod"))
	}
	if query.Get("Signature") == "" {
		t.Fatal("Signature is empty")
	}
	if req.Method != "POST" {
		t.Fatalf("method = %s, want POST", req.Method)
	}
}

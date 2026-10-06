package s3

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

// Test vectors from the AWS documentation for Signature Version 4 on S3.
func awsClient() *Client {
	return &Client{
		Region: "us-east-1", AccessKey: "AKIAIOSFODNN7EXAMPLE", SecretKey: "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
		Now: func() time.Time { return time.Date(2013, 5, 24, 0, 0, 0, 0, time.UTC) },
	}
}

func TestPresignVector(t *testing.T) {
	u, err := awsClient().Presign("GET", "https://examplebucket.s3.amazonaws.com/test.txt", 86400*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(u, "X-Amz-Signature=aeeed9bbccd4d02ee5c0109b86d86835f995330da4c265957d157751f604d404") {
		t.Fatalf("presigned URL signature mismatch: %s", u)
	}
}

func TestHeaderSignVector(t *testing.T) {
	req, _ := http.NewRequest("GET", "https://examplebucket.s3.amazonaws.com/test.txt", nil)
	req.Header.Set("Range", "bytes=0-9")
	awsClient().Sign(req, "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855")
	want := "AWS4-HMAC-SHA256 Credential=AKIAIOSFODNN7EXAMPLE/20130524/us-east-1/s3/aws4_request, SignedHeaders=host;range;x-amz-content-sha256;x-amz-date, Signature=f0e8bdb87c964420e857bd35b5d6ed310bd44f0170aba48dd91039c6036bdb41"
	if got := req.Header.Get("Authorization"); got != want {
		t.Fatalf("authorization:\n got %s\nwant %s", got, want)
	}
}

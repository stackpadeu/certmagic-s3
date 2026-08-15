package s3

import (
	"errors"
	"fmt"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	s3sdk "github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"
)

func TestS3_objName(t *testing.T) {
	tests := []struct {
		name     string
		prefix   string
		key      string
		expected string
	}{
		{
			name:     "empty prefix",
			prefix:   "",
			key:      "test.key",
			expected: "test.key",
		},
		{
			name:     "with prefix",
			prefix:   "acme",
			key:      "test.key",
			expected: "acme/test.key",
		},
		{
			name:     "slash normalization",
			prefix:   "//acme//",
			key:      "//test.key",
			expected: "acme/test.key",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s3 := &S3{Prefix: tt.prefix}
			result := s3.objName(tt.key)
			if result != tt.expected {
				t.Errorf("objName() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestS3_objLockName(t *testing.T) {
	s3 := &S3{Prefix: "acme"}
	key := "test.key"
	expected := "acme/test.key.lock"

	result := s3.objLockName(key)
	if result != expected {
		t.Errorf("objLockName() = %v, want %v", result, expected)
	}
}

func TestConditionFailed(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{
			name:     "another instance holds the lock",
			err:      &smithy.GenericAPIError{Code: "PreconditionFailed"},
			expected: true,
		},
		{
			name:     "raced another conditional write",
			err:      &smithy.GenericAPIError{Code: "ConditionalRequestConflict"},
			expected: true,
		},
		{
			name:     "lock released before we could take it over",
			err:      &types.NoSuchKey{},
			expected: true,
		},
		{
			name:     "wrapped api error",
			err:      fmt.Errorf("put lock file: %w", &smithy.GenericAPIError{Code: "PreconditionFailed"}),
			expected: true,
		},
		{
			name:     "unrelated api error",
			err:      &types.NoSuchBucket{},
			expected: false,
		},
		{
			name:     "plain error",
			err:      errors.New("connection reset"),
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := conditionFailed(tt.err)
			if result != tt.expected {
				t.Errorf("conditionFailed() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestEntityTag(t *testing.T) {
	tests := []struct {
		name     string
		etag     *string
		expected string
	}{
		{
			name:     "quoted, as GetObject returns it",
			etag:     aws.String(`"6654c734ccab8f440ff0825eb443dc7f"`),
			expected: "6654c734ccab8f440ff0825eb443dc7f",
		},
		{
			name:     "multipart etag keeps its part count",
			etag:     aws.String(`"6654c734ccab8f440ff0825eb443dc7f-3"`),
			expected: "6654c734ccab8f440ff0825eb443dc7f-3",
		},
		{
			name:     "already unquoted",
			etag:     aws.String("6654c734ccab8f440ff0825eb443dc7f"),
			expected: "6654c734ccab8f440ff0825eb443dc7f",
		},
		{
			name:     "absent",
			etag:     nil,
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := entityTag(tt.etag)
			if result != tt.expected {
				t.Errorf("entityTag() = %q, want %q", result, tt.expected)
			}
		})
	}
}

// TestIfUnchangedSendsUnquotedETag guards the header form directly: Ceph
// answers 412 to a quoted If-Match, which leaves an abandoned lock impossible
// to take over.
func TestIfUnchangedSendsUnquotedETag(t *testing.T) {
	input := &s3sdk.PutObjectInput{}
	ifUnchanged(aws.String(`"6654c734ccab8f440ff0825eb443dc7f"`))(input)

	expected := "6654c734ccab8f440ff0825eb443dc7f"
	if aws.ToString(input.IfMatch) != expected {
		t.Errorf("IfMatch = %q, want %q", aws.ToString(input.IfMatch), expected)
	}
}

func TestIfNotExistsSendsWildcard(t *testing.T) {
	input := &s3sdk.PutObjectInput{}
	ifNotExists(input)

	if aws.ToString(input.IfNoneMatch) != "*" {
		t.Errorf("IfNoneMatch = %q, want %q", aws.ToString(input.IfNoneMatch), "*")
	}
	if input.IfMatch != nil {
		t.Errorf("IfMatch = %q, want it unset", aws.ToString(input.IfMatch))
	}
}

func TestS3_UsePathStyleConfiguration(t *testing.T) {
	tests := []struct {
		name            string
		endpoint        string
		usePathStyle    bool
		expectPathStyle bool
	}{
		{
			name:            "default AWS (no custom endpoint)",
			endpoint:        "",
			usePathStyle:    false,
			expectPathStyle: false,
		},
		{
			name:            "explicit path style enabled",
			endpoint:        "",
			usePathStyle:    true,
			expectPathStyle: true,
		},
		{
			name:            "custom endpoint forces path style",
			endpoint:        "https://minio.example.com",
			usePathStyle:    false,
			expectPathStyle: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s3 := &S3{
				Endpoint:     tt.endpoint,
				UsePathStyle: tt.usePathStyle,
			}

			endpoint := tt.endpoint
			shouldUsePathStyle := s3.UsePathStyle || endpoint != ""

			if shouldUsePathStyle != tt.expectPathStyle {
				t.Errorf("UsePathStyle logic = %v, want %v", shouldUsePathStyle, tt.expectPathStyle)
			}
		})
	}
}

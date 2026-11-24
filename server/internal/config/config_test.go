package config

import (
	"testing"
	"time"
)

// baseEnv is the smallest environment Load accepts, with no VIDEO_* set.
func baseEnv(t *testing.T) {
	t.Helper()
	for k, v := range map[string]string{
		"AUTH_MODE": "dev", "STORE": "memory", "MODEL_CLIENT": "echo", "PORT": "",
		"VIDEO_S3_ENDPOINT": "", "VIDEO_S3_REGION": "", "VIDEO_S3_BUCKET": "", "VIDEO_S3_PREFIX": "",
		"VIDEO_S3_ACCESS_KEY": "", "VIDEO_S3_SECRET_KEY": "", "VIDEO_CACHE_TTL": "",
	} {
		t.Setenv(k, v)
	}
}

func load(t *testing.T) *Config {
	t.Helper()
	c, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return c
}

func check[T comparable](t *testing.T, name string, got, want T) {
	t.Helper()
	if got != want {
		t.Errorf("%s = %v, want %v", name, got, want)
	}
}

// Nothing about the portfolio is required: Load succeeds without it.
func TestVideoDefaults(t *testing.T) {
	baseEnv(t)
	c := load(t)
	check(t, "VideoS3Endpoint", c.VideoS3Endpoint, "https://us-lax-1.linodeobjects.com")
	check(t, "VideoS3Region", c.VideoS3Region, "us-lax-1")
	check(t, "VideoS3Bucket", c.VideoS3Bucket, "")
	check(t, "VideoS3Prefix", c.VideoS3Prefix, "videos/")
	check(t, "VideoS3AccessKey", c.VideoS3AccessKey, "")
	check(t, "VideoS3SecretKey", c.VideoS3SecretKey, "")
	check(t, "VideoCacheTTL", c.VideoCacheTTL, 5*time.Minute)
}

func TestVideoOverrides(t *testing.T) {
	baseEnv(t)
	t.Setenv("VIDEO_S3_ENDPOINT", "http://localhost:9000")
	t.Setenv("VIDEO_S3_REGION", "us-east-1")
	t.Setenv("VIDEO_S3_BUCKET", "expona-images")
	t.Setenv("VIDEO_S3_PREFIX", "films/")
	t.Setenv("VIDEO_S3_ACCESS_KEY", "access")
	t.Setenv("VIDEO_S3_SECRET_KEY", "secret")
	t.Setenv("VIDEO_CACHE_TTL", "90s")
	c := load(t)
	check(t, "VideoS3Endpoint", c.VideoS3Endpoint, "http://localhost:9000")
	check(t, "VideoS3Region", c.VideoS3Region, "us-east-1")
	check(t, "VideoS3Bucket", c.VideoS3Bucket, "expona-images")
	check(t, "VideoS3Prefix", c.VideoS3Prefix, "films/")
	check(t, "VideoS3AccessKey", c.VideoS3AccessKey, "access")
	check(t, "VideoS3SecretKey", c.VideoS3SecretKey, "secret")
	check(t, "VideoCacheTTL", c.VideoCacheTTL, 90*time.Second)
}

func TestVideoPrefixGetsTrailingSlash(t *testing.T) {
	baseEnv(t)
	t.Setenv("VIDEO_S3_PREFIX", "films")
	check(t, "VideoS3Prefix", load(t).VideoS3Prefix, "films/")
}

func TestVideoCacheTTLFallsBack(t *testing.T) {
	for _, v := range []string{"banana", "5", "0", "0s", "-1m"} {
		t.Run(v, func(t *testing.T) {
			baseEnv(t)
			t.Setenv("VIDEO_CACHE_TTL", v)
			check(t, "VideoCacheTTL", load(t).VideoCacheTTL, 5*time.Minute)
		})
	}
}

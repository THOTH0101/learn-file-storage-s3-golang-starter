package main

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/bootdotdev/learn-file-storage-s3-golang-starter/internal/database"
)

func (cfg *apiConfig) dbVideoToSignedVideo(video database.Video) (database.Video, error) {
	parts := strings.Split(*video.VideoURL, ",")
	if len(parts) != 2 {
		return database.Video{}, fmt.Errorf("invalid video URL format in database; expected 'bucket,key', got: %s", video.VideoURL)
	}

	bucket := strings.TrimSpace(parts[0])
	key := strings.TrimSpace(parts[1])
	expiration := 1 * time.Hour

	presignedURL, err := generatePresignedURL(cfg.s3Client, bucket, key, expiration)
	if err != nil {
		return video, err
	}

	video.VideoURL = &presignedURL
	return video, nil
}

func processVideoForFastStart(filePath string) (string, error) {
	outputPath := filePath + ".processing"
	args := []string{
		"-i", filePath, "-c", "copy", "-movflags", "faststart", "-f", "mp4", outputPath,
	}

	cmd := exec.Command("ffmpeg", args...)
	if err := cmd.Run(); err != nil {
		os.Remove(outputPath)
		return "", err
	}

	return outputPath, nil
}

func getVideoPrefix(aspectRatio string) string {
	switch aspectRatio {
	case "16:9":
		return "landscape"
	case "9:16":
		return "portrait"
	default:
		return "other"
	}
}

func getVideoAspectRatio(filePath string) (string, error) {
	args := []string{
		"-v", "error",
		"-print_format", "json",
		"-show_streams",
		"-select_streams", "v",
		filePath,
	}

	cmd := exec.Command("ffprobe", args...)
	var stdoutBuf bytes.Buffer
	cmd.Stdout = &stdoutBuf
	if err := cmd.Run(); err != nil {
		return "", err
	}

	type ffprobeOutput struct {
		Streams []struct {
			Width  int `json:"width"`
			Height int `json:"height"`
		} `json:"streams"`
	}

	var output ffprobeOutput
	if err := json.Unmarshal(stdoutBuf.Bytes(), &output); err != nil {
		return "", err
	}

	if len(output.Streams) == 0 {
		return "", fmt.Errorf("no streams found in video file: %s", filePath)
	}

	width := output.Streams[0].Width
	height := output.Streams[0].Height

	if width == 0 || height == 0 {
		return "", fmt.Errorf("invalid video dimensions: %dx%d", width, height)
	}

	ratio := float64(width) / float64(height)

	const target16x9 = 16.0 / 9.0 // ~1.777
	const target9x16 = 9.0 / 16.0 // ~0.562
	const tolerance = 0.01

	// Check if the calculated ratio is within the tolerance boundaries
	if math.Abs(ratio-target16x9) <= tolerance {
		return "16:9", nil
	}
	if math.Abs(ratio-target9x16) <= tolerance {
		return "9:16", nil
	}

	return "other", nil
}

func (cfg apiConfig) ensureAssetsDir() error {
	if _, err := os.Stat(cfg.assetsRoot); os.IsNotExist(err) {
		return os.Mkdir(cfg.assetsRoot, 0o755)
	}
	return nil
}

func getAssetPath(mediaType string) string {
	base := make([]byte, 32)
	_, err := rand.Read(base)
	if err != nil {
		panic("failed to generate random bytes")
	}
	id := base64.RawURLEncoding.EncodeToString(base)

	ext := mediaTypeToExt(mediaType)
	return fmt.Sprintf("%s%s", id, ext)
}

func (cfg apiConfig) getObjectURL(key string) string {
	return fmt.Sprintf("https://%s.s3.%s.amazonaws.com/%s", cfg.s3Bucket, cfg.s3Region, key)
}

func (cfg apiConfig) getAssetDiskPath(assetPath string) string {
	return filepath.Join(cfg.assetsRoot, assetPath)
}

func (cfg apiConfig) getAssetURL(assetPath string) string {
	return fmt.Sprintf("http://localhost:%s/assets/%s", cfg.port, assetPath)
}

func mediaTypeToExt(mediaType string) string {
	parts := strings.Split(mediaType, "/")
	if len(parts) != 2 {
		return ".bin"
	}
	return "." + parts[1]
}

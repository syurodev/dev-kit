package bridge

import (
	"testing"

	"devkit/internal/app"
	"devkit/internal/kafka"
)

func TestKafkaListAndCreateProfile(t *testing.T) {
	container := app.NewContainer()
	service := NewAppService(container)
	if _, err := service.SetupVault("master-password"); err != nil {
		t.Fatalf("SetupVault: %v", err)
	}

	created, err := service.KafkaCreateProfile(kafka.CreateProfileInput{
		Name:             "local broker",
		BootstrapServers: []string{"127.0.0.1:9092"},
		Env:              "dev",
	})
	if err != nil {
		t.Fatalf("KafkaCreateProfile: %v", err)
	}
	if created.ID == "" || created.Name != "local broker" {
		t.Fatalf("created profile = %+v", created)
	}

	profiles, err := service.KafkaListProfiles()
	if err != nil {
		t.Fatalf("KafkaListProfiles: %v", err)
	}
	if len(profiles) != 1 {
		t.Fatalf("len(profiles) = %d, want 1", len(profiles))
	}
	if profiles[0].ID != created.ID {
		t.Fatalf("listed profile id = %q, want %q", profiles[0].ID, created.ID)
	}
}

func TestKafkaBridgeNilServiceDoesNotPanic(t *testing.T) {
	container := app.NewContainer()
	container.Kafka = nil
	service := NewAppService(container)
	if _, err := service.SetupVault("master-password"); err != nil {
		t.Fatalf("SetupVault: %v", err)
	}

	_, err := service.KafkaListProfiles()
	if err == nil || err.Error() != "internal_error: Kafka is not available." {
		t.Fatalf("KafkaListProfiles error = %v, want internal_error unavailable", err)
	}
}

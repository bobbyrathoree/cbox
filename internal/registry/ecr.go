package registry

import (
	"context"
	"encoding/base64"
	"fmt"
	"os/exec"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ecr"
	"github.com/aws/aws-sdk-go-v2/service/ecr/types"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"github.com/bobbyrathore/cbox/internal/config"
	"github.com/bobbyrathore/cbox/internal/output"
)

// ECRRegistry implements the Registry interface for AWS ECR.
type ECRRegistry struct {
	client      *ecr.Client
	region      string
	accountID   string
	projectName string
	console     *output.Console
	registryURL string
}

// NewECR creates a new ECR registry client.
func NewECR(cfg *config.RegistryConfig, projectName string, console *output.Console) (*ECRRegistry, error) {
	region := cfg.Region
	if region == "" {
		region = "us-east-1" // Default region
	}

	// Load AWS config
	awsCfg, err := awsconfig.LoadDefaultConfig(context.Background(),
		awsconfig.WithRegion(region),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config: %w", err)
	}

	// Get account ID if not provided
	accountID := cfg.AccountID
	if accountID == "" {
		stsClient := sts.NewFromConfig(awsCfg)
		identity, err := stsClient.GetCallerIdentity(context.Background(), &sts.GetCallerIdentityInput{})
		if err != nil {
			return nil, fmt.Errorf("failed to get AWS account ID: %w", err)
		}
		accountID = *identity.Account
	}

	registryURL := fmt.Sprintf("%s.dkr.ecr.%s.amazonaws.com", accountID, region)

	return &ECRRegistry{
		client:      ecr.NewFromConfig(awsCfg),
		region:      region,
		accountID:   accountID,
		projectName: projectName,
		console:     console,
		registryURL: registryURL,
	}, nil
}

// Authenticate authenticates Docker with ECR.
func (r *ECRRegistry) Authenticate(ctx context.Context) error {
	// Get authorization token from ECR
	result, err := r.client.GetAuthorizationToken(ctx, &ecr.GetAuthorizationTokenInput{})
	if err != nil {
		return fmt.Errorf("failed to get ECR authorization token: %w", err)
	}

	if len(result.AuthorizationData) == 0 {
		return fmt.Errorf("no authorization data returned from ECR")
	}

	authData := result.AuthorizationData[0]
	token, err := base64.StdEncoding.DecodeString(*authData.AuthorizationToken)
	if err != nil {
		return fmt.Errorf("failed to decode authorization token: %w", err)
	}

	// Token format is "AWS:password"
	parts := strings.SplitN(string(token), ":", 2)
	if len(parts) != 2 {
		return fmt.Errorf("invalid authorization token format")
	}
	password := parts[1]

	// Login to Docker
	cmd := exec.CommandContext(ctx, "docker", "login",
		"--username", "AWS",
		"--password-stdin",
		r.registryURL,
	)
	cmd.Stdin = strings.NewReader(password)

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker login failed: %w\n%s", err, string(output))
	}

	r.console.Success("Authenticated with ECR: %s", r.registryURL)
	return nil
}

// Push pushes an image to ECR.
func (r *ECRRegistry) Push(ctx context.Context, localImage, tag string) (string, error) {
	// Extract service name from local image (format: projectname-servicename:tag)
	// Remove tag if present
	imagePart := strings.Split(localImage, ":")[0]

	// Use the project name to properly extract service name
	// Expected format: {projectName}-{serviceName}
	serviceName := imagePart
	prefix := r.projectName + "-"
	if strings.HasPrefix(imagePart, prefix) {
		serviceName = imagePart[len(prefix):]
	}

	// Get full ECR image name
	fullImageName := r.GetFullImageName(r.projectName, serviceName, tag)

	// Tag the image
	r.console.Info("Tagging %s as %s", localImage, fullImageName)
	tagCmd := exec.CommandContext(ctx, "docker", "tag", localImage, fullImageName)
	if output, err := tagCmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("failed to tag image: %w\n%s", err, string(output))
	}

	// Push the image
	r.console.Info("Pushing %s...", fullImageName)
	pushCmd := exec.CommandContext(ctx, "docker", "push", fullImageName)
	if output, err := pushCmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("failed to push image: %w\n%s", err, string(output))
	}

	return fullImageName, nil
}

// GetFullImageName returns the full ECR image name for a service.
func (r *ECRRegistry) GetFullImageName(projectName, serviceName, tag string) string {
	repoName := fmt.Sprintf("%s-%s", projectName, serviceName)
	return fmt.Sprintf("%s/%s:%s", r.registryURL, repoName, tag)
}

// EnsureRepository ensures the ECR repository exists.
func (r *ECRRegistry) EnsureRepository(ctx context.Context, repositoryName string) error {
	// Try to describe the repository
	_, err := r.client.DescribeRepositories(ctx, &ecr.DescribeRepositoriesInput{
		RepositoryNames: []string{repositoryName},
	})

	if err == nil {
		// Repository exists
		return nil
	}

	// Check if it's a "not found" error
	if !strings.Contains(err.Error(), "RepositoryNotFoundException") {
		return fmt.Errorf("failed to check repository: %w", err)
	}

	// Create the repository
	r.console.Info("Creating ECR repository: %s", repositoryName)
	_, err = r.client.CreateRepository(ctx, &ecr.CreateRepositoryInput{
		RepositoryName: aws.String(repositoryName),
		ImageScanningConfiguration: &types.ImageScanningConfiguration{
			ScanOnPush: true,
		},
		ImageTagMutability: types.ImageTagMutabilityMutable,
	})

	if err != nil {
		return fmt.Errorf("failed to create repository: %w", err)
	}

	r.console.Success("Created ECR repository: %s", repositoryName)
	return nil
}

// GetRepositoryName returns the repository name for a service.
func (r *ECRRegistry) GetRepositoryName(serviceName string) string {
	return fmt.Sprintf("%s-%s", r.projectName, serviceName)
}

package deployer

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/aws/aws-sdk-go-v2/service/ecs"
	"github.com/aws/aws-sdk-go-v2/service/ecs/types"
	"github.com/bobbyrathore/cbox/internal/config"
	"github.com/bobbyrathore/cbox/internal/output"
)

// ECSDeployer implements the Deployer interface for AWS ECS/Fargate.
type ECSDeployer struct {
	ecsClient   *ecs.Client
	ec2Client   *ec2.Client
	config      *config.ECSConfig
	projectName string
	console     *output.Console
	clusterName string
}

// NewECS creates a new ECS deployer.
func NewECS(cfg *config.ECSConfig, projectName string, console *output.Console) (*ECSDeployer, error) {
	region := cfg.Region
	if region == "" {
		region = "us-east-1"
	}

	awsCfg, err := awsconfig.LoadDefaultConfig(context.Background(),
		awsconfig.WithRegion(region),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config: %w", err)
	}

	clusterName := cfg.Cluster
	if clusterName == "" {
		clusterName = fmt.Sprintf("cbox-%s", projectName)
	}

	return &ECSDeployer{
		ecsClient:   ecs.NewFromConfig(awsCfg),
		ec2Client:   ec2.NewFromConfig(awsCfg),
		config:      cfg,
		projectName: projectName,
		console:     console,
		clusterName: clusterName,
	}, nil
}

// Deploy deploys services to ECS.
func (d *ECSDeployer) Deploy(ctx context.Context, services []ServiceDeployConfig) error {
	// Ensure cluster exists
	if err := d.ensureCluster(ctx); err != nil {
		return fmt.Errorf("failed to ensure cluster: %w", err)
	}

	// Get network configuration
	subnets, securityGroups, err := d.getNetworkConfig(ctx)
	if err != nil {
		return fmt.Errorf("failed to get network configuration: %w", err)
	}

	// Deploy each service
	for _, svc := range services {
		d.console.Info("Deploying %s...", svc.Name)

		// Register task definition
		taskDefArn, err := d.registerTaskDefinition(ctx, svc)
		if err != nil {
			return fmt.Errorf("failed to register task definition for %s: %w", svc.Name, err)
		}
		d.console.Info("  Registered task definition: %s", taskDefArn)

		// Create or update service
		serviceName := fmt.Sprintf("%s-%s", d.projectName, svc.Name)
		exists, err := d.serviceExists(ctx, serviceName)
		if err != nil {
			return fmt.Errorf("failed to check service: %w", err)
		}

		if exists {
			if err := d.updateService(ctx, serviceName, taskDefArn, svc.DesiredCount); err != nil {
				return fmt.Errorf("failed to update service %s: %w", svc.Name, err)
			}
			d.console.Info("  Updated service: %s", serviceName)
		} else {
			if err := d.createService(ctx, serviceName, taskDefArn, svc, subnets, securityGroups); err != nil {
				return fmt.Errorf("failed to create service %s: %w", svc.Name, err)
			}
			d.console.Info("  Created service: %s", serviceName)
		}

		d.console.Success("Deployed %s", svc.Name)
	}

	return nil
}

// Status returns deployment status.
func (d *ECSDeployer) Status(ctx context.Context) (*DeploymentStatus, error) {
	result, err := d.ecsClient.ListServices(ctx, &ecs.ListServicesInput{
		Cluster: aws.String(d.clusterName),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list services: %w", err)
	}

	if len(result.ServiceArns) == 0 {
		return &DeploymentStatus{Services: []ServiceStatus{}}, nil
	}

	desc, err := d.ecsClient.DescribeServices(ctx, &ecs.DescribeServicesInput{
		Cluster:  aws.String(d.clusterName),
		Services: result.ServiceArns,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to describe services: %w", err)
	}

	var statuses []ServiceStatus
	for _, svc := range desc.Services {
		statuses = append(statuses, ServiceStatus{
			Name:           *svc.ServiceName,
			Status:         *svc.Status,
			DesiredCount:   int(svc.DesiredCount),
			RunningCount:   int(svc.RunningCount),
			PendingCount:   int(svc.PendingCount),
			TaskDefinition: *svc.TaskDefinition,
		})
	}

	return &DeploymentStatus{Services: statuses}, nil
}

// Wait waits for a service to stabilize.
func (d *ECSDeployer) Wait(ctx context.Context, serviceName string) error {
	fullName := fmt.Sprintf("%s-%s", d.projectName, serviceName)
	d.console.Info("Waiting for %s to stabilize...", serviceName)

	waiter := ecs.NewServicesStableWaiter(d.ecsClient)
	err := waiter.Wait(ctx, &ecs.DescribeServicesInput{
		Cluster:  aws.String(d.clusterName),
		Services: []string{fullName},
	}, 10*time.Minute)

	if err != nil {
		return fmt.Errorf("service did not stabilize: %w", err)
	}

	d.console.Success("Service %s is stable", serviceName)
	return nil
}

// ensureCluster ensures the ECS cluster exists.
func (d *ECSDeployer) ensureCluster(ctx context.Context) error {
	// Try to describe the cluster
	result, err := d.ecsClient.DescribeClusters(ctx, &ecs.DescribeClustersInput{
		Clusters: []string{d.clusterName},
	})
	if err != nil {
		return err
	}

	// Check if cluster exists and is active
	for _, cluster := range result.Clusters {
		if *cluster.ClusterName == d.clusterName && *cluster.Status == "ACTIVE" {
			return nil
		}
	}

	// Create the cluster
	d.console.Info("Creating ECS cluster: %s", d.clusterName)
	_, err = d.ecsClient.CreateCluster(ctx, &ecs.CreateClusterInput{
		ClusterName: aws.String(d.clusterName),
		CapacityProviders: []string{
			"FARGATE",
			"FARGATE_SPOT",
		},
		DefaultCapacityProviderStrategy: []types.CapacityProviderStrategyItem{
			{
				CapacityProvider: aws.String("FARGATE"),
				Weight:           1,
				Base:             1,
			},
		},
	})
	if err != nil {
		return fmt.Errorf("failed to create cluster: %w", err)
	}

	d.console.Success("Created ECS cluster: %s", d.clusterName)
	return nil
}

// getNetworkConfig returns the subnets and security groups for deployment.
func (d *ECSDeployer) getNetworkConfig(ctx context.Context) ([]string, []string, error) {
	subnets := d.config.Subnets
	securityGroups := d.config.SecurityGroups

	// If subnets not specified, discover default VPC subnets
	if len(subnets) == 0 {
		d.console.Info("Discovering default VPC subnets...")

		// Get default VPC
		vpcs, err := d.ec2Client.DescribeVpcs(ctx, &ec2.DescribeVpcsInput{
			Filters: []ec2types.Filter{
				{
					Name:   aws.String("is-default"),
					Values: []string{"true"},
				},
			},
		})
		if err != nil {
			return nil, nil, fmt.Errorf("failed to describe VPCs: %w", err)
		}

		if len(vpcs.Vpcs) == 0 {
			return nil, nil, fmt.Errorf("no default VPC found; specify subnets in cbox.yaml")
		}

		vpcID := *vpcs.Vpcs[0].VpcId

		// Get subnets in the VPC
		subnetResult, err := d.ec2Client.DescribeSubnets(ctx, &ec2.DescribeSubnetsInput{
			Filters: []ec2types.Filter{
				{
					Name:   aws.String("vpc-id"),
					Values: []string{vpcID},
				},
			},
		})
		if err != nil {
			return nil, nil, fmt.Errorf("failed to describe subnets: %w", err)
		}

		for _, subnet := range subnetResult.Subnets {
			subnets = append(subnets, *subnet.SubnetId)
		}

		if len(subnets) == 0 {
			return nil, nil, fmt.Errorf("no subnets found in default VPC")
		}

		d.console.Info("  Using VPC: %s with %d subnets", vpcID, len(subnets))
	}

	// If security groups not specified, use default
	if len(securityGroups) == 0 {
		// Get default security group for the VPC
		// For now, we'll let ECS use the default
		d.console.Info("  Using default security group")
	}

	return subnets, securityGroups, nil
}

// registerTaskDefinition registers a new task definition.
func (d *ECSDeployer) registerTaskDefinition(ctx context.Context, svc ServiceDeployConfig) (string, error) {
	// Build container definition
	containerDef := types.ContainerDefinition{
		Name:      aws.String(svc.Name),
		Image:     aws.String(svc.Image),
		Essential: aws.Bool(true),
		PortMappings: []types.PortMapping{
			{
				ContainerPort: aws.Int32(int32(svc.Port)),
				Protocol:      types.TransportProtocolTcp,
			},
		},
		LogConfiguration: &types.LogConfiguration{
			LogDriver: types.LogDriverAwslogs,
			Options: map[string]string{
				"awslogs-group":         fmt.Sprintf("/ecs/%s-%s", d.projectName, svc.Name),
				"awslogs-region":        d.config.Region,
				"awslogs-stream-prefix": "ecs",
				"awslogs-create-group":  "true",
			},
		},
	}

	// Add environment variables
	if len(svc.Env) > 0 {
		var envVars []types.KeyValuePair
		for k, v := range svc.Env {
			envVars = append(envVars, types.KeyValuePair{
				Name:  aws.String(k),
				Value: aws.String(v),
			})
		}
		containerDef.Environment = envVars
	}

	// Add health check if configured
	if svc.HealthCheckPath != "" && svc.Port > 0 {
		containerDef.HealthCheck = &types.HealthCheck{
			Command: []string{
				"CMD-SHELL",
				fmt.Sprintf("wget --no-verbose --tries=1 --spider http://localhost:%d%s || exit 1", svc.Port, svc.HealthCheckPath),
			},
			Interval:    aws.Int32(30),
			Timeout:     aws.Int32(5),
			Retries:     aws.Int32(3),
			StartPeriod: aws.Int32(60),
		}
	}

	// Validate CPU/Memory combinations for Fargate
	cpu, memory := validateFargateResources(svc.CPU, svc.Memory)

	// Register the task definition
	executionRole := d.getExecutionRoleArn()
	if executionRole == nil {
		return "", fmt.Errorf("execution_role_arn is required for ECS deployment; set deploy.ecs.execution_role_arn in cbox.yaml (e.g., arn:aws:iam::<account-id>:role/ecsTaskExecutionRole)")
	}

	family := fmt.Sprintf("%s-%s", d.projectName, svc.Name)
	result, err := d.ecsClient.RegisterTaskDefinition(ctx, &ecs.RegisterTaskDefinitionInput{
		Family:                  aws.String(family),
		ContainerDefinitions:    []types.ContainerDefinition{containerDef},
		RequiresCompatibilities: []types.Compatibility{types.CompatibilityFargate},
		NetworkMode:             types.NetworkModeAwsvpc,
		Cpu:                     aws.String(fmt.Sprintf("%d", cpu)),
		Memory:                  aws.String(fmt.Sprintf("%d", memory)),
		ExecutionRoleArn:        executionRole,
		TaskRoleArn:             d.getTaskRoleArn(),
	})
	if err != nil {
		return "", err
	}

	return *result.TaskDefinition.TaskDefinitionArn, nil
}

// serviceExists checks if an ECS service exists.
func (d *ECSDeployer) serviceExists(ctx context.Context, serviceName string) (bool, error) {
	result, err := d.ecsClient.DescribeServices(ctx, &ecs.DescribeServicesInput{
		Cluster:  aws.String(d.clusterName),
		Services: []string{serviceName},
	})
	if err != nil {
		return false, err
	}

	for _, svc := range result.Services {
		if *svc.ServiceName == serviceName && *svc.Status != "INACTIVE" {
			return true, nil
		}
	}

	return false, nil
}

// createService creates a new ECS service.
func (d *ECSDeployer) createService(ctx context.Context, serviceName, taskDefArn string, svc ServiceDeployConfig, subnets, securityGroups []string) error {
	input := &ecs.CreateServiceInput{
		Cluster:        aws.String(d.clusterName),
		ServiceName:    aws.String(serviceName),
		TaskDefinition: aws.String(taskDefArn),
		DesiredCount:   aws.Int32(int32(svc.DesiredCount)),
		LaunchType:     types.LaunchTypeFargate,
		NetworkConfiguration: &types.NetworkConfiguration{
			AwsvpcConfiguration: &types.AwsVpcConfiguration{
				Subnets:        subnets,
				SecurityGroups: securityGroups,
				AssignPublicIp: types.AssignPublicIpEnabled,
			},
		},
		DeploymentConfiguration: &types.DeploymentConfiguration{
			MaximumPercent:        aws.Int32(200),
			MinimumHealthyPercent: aws.Int32(100),
		},
	}

	if !d.config.AssignPublicIP {
		input.NetworkConfiguration.AwsvpcConfiguration.AssignPublicIp = types.AssignPublicIpDisabled
	}

	_, err := d.ecsClient.CreateService(ctx, input)
	return err
}

// updateService updates an existing ECS service.
func (d *ECSDeployer) updateService(ctx context.Context, serviceName, taskDefArn string, desiredCount int) error {
	_, err := d.ecsClient.UpdateService(ctx, &ecs.UpdateServiceInput{
		Cluster:            aws.String(d.clusterName),
		Service:            aws.String(serviceName),
		TaskDefinition:     aws.String(taskDefArn),
		DesiredCount:       aws.Int32(int32(desiredCount)),
		ForceNewDeployment: true,
	})
	return err
}

// getExecutionRoleArn returns the execution role ARN.
func (d *ECSDeployer) getExecutionRoleArn() *string {
	if d.config.ExecutionRoleARN != "" {
		return aws.String(d.config.ExecutionRoleARN)
	}
	return nil
}

// getTaskRoleArn returns the task role ARN.
func (d *ECSDeployer) getTaskRoleArn() *string {
	if d.config.TaskRoleARN != "" {
		return aws.String(d.config.TaskRoleARN)
	}
	return nil
}

// validateFargateResources ensures CPU/Memory are valid Fargate combinations.
func validateFargateResources(cpu, memory int) (int, int) {
	// Valid Fargate CPU/Memory combinations
	// https://docs.aws.amazon.com/AmazonECS/latest/developerguide/task-cpu-memory-error.html
	validCombinations := map[int][]int{
		256:  {512, 1024, 2048},
		512:  {1024, 2048, 3072, 4096},
		1024: {2048, 3072, 4096, 5120, 6144, 7168, 8192},
		2048: {4096, 5120, 6144, 7168, 8192, 9216, 10240, 11264, 12288, 13312, 14336, 15360, 16384},
		4096: {8192, 9216, 10240, 11264, 12288, 13312, 14336, 15360, 16384, 17408, 18432, 19456, 20480, 21504, 22528, 23552, 24576, 25600, 26624, 27648, 28672, 29696, 30720},
	}

	// Default to smallest valid combination
	if cpu == 0 {
		cpu = 256
	}
	if memory == 0 {
		memory = 512
	}

	// Find closest valid CPU
	validCPUs := []int{256, 512, 1024, 2048, 4096}
	selectedCPU := 256
	for _, c := range validCPUs {
		if c >= cpu {
			selectedCPU = c
			break
		}
		selectedCPU = c
	}

	// Find closest valid memory for the selected CPU
	validMemories := validCombinations[selectedCPU]
	selectedMemory := validMemories[0]
	for _, m := range validMemories {
		if m >= memory {
			selectedMemory = m
			break
		}
		selectedMemory = m
	}

	return selectedCPU, selectedMemory
}

// GetClusterName returns the ECS cluster name.
func (d *ECSDeployer) GetClusterName() string {
	return d.clusterName
}

// PrintDryRun outputs what would be deployed.
func (d *ECSDeployer) PrintDryRun(services []ServiceDeployConfig) {
	d.console.Header("Dry run - would deploy to ECS:")
	d.console.Info("  Cluster: %s", d.clusterName)
	d.console.Info("  Region: %s", d.config.Region)
	d.console.Newline()

	for _, svc := range services {
		cpu, memory := validateFargateResources(svc.CPU, svc.Memory)
		d.console.Info("  Service: %s", svc.Name)
		d.console.Info("    Image: %s", svc.Image)
		d.console.Info("    Port: %d", svc.Port)
		d.console.Info("    CPU: %d", cpu)
		d.console.Info("    Memory: %d MB", memory)
		d.console.Info("    Replicas: %d", svc.DesiredCount)
		if svc.HealthCheckPath != "" {
			d.console.Info("    Health check: %s", svc.HealthCheckPath)
		}
		if len(svc.Env) > 0 {
			d.console.Info("    Environment: %d variables", len(svc.Env))
			for k := range svc.Env {
				// Don't print values for security
				if strings.Contains(strings.ToLower(k), "secret") || strings.Contains(strings.ToLower(k), "password") {
					d.console.Info("      %s: [REDACTED]", k)
				}
			}
		}
		d.console.Newline()
	}
}

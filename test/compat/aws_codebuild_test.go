package compat_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/codebuild"
	"github.com/aws/aws-sdk-go-v2/service/codebuild/types"
	"github.com/aws/smithy-go"
	compataws "github.com/homeport/homeport/internal/app/compat/aws"
	"github.com/homeport/homeport/internal/domain/authz"
)

func TestCodeBuildCompatibilityAdapterWithAWSSDK(t *testing.T) {
	server := httptest.NewServer(compataws.NewCodeBuildAdapter())
	defer server.Close()
	client := codebuild.NewFromConfig(aws.Config{Region: "us-east-1", Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", "")}, func(o *codebuild.Options) { o.BaseEndpoint = aws.String(server.URL) })
	ctx := context.Background()
	created, err := client.CreateProject(ctx, &codebuild.CreateProjectInput{Name: aws.String("orders"), ServiceRole: aws.String("arn:aws:iam::000000000000:role/codebuild"), Artifacts: &types.ProjectArtifacts{Type: types.ArtifactsTypeNoArtifacts}, Environment: &types.ProjectEnvironment{ComputeType: types.ComputeTypeBuildGeneral1Small, Image: aws.String("aws/codebuild/standard:7.0"), Type: types.EnvironmentTypeLinuxContainer}, Source: &types.ProjectSource{Type: types.SourceTypeGithub, Location: aws.String("https://github.com/example/orders")}})
	if err != nil || aws.ToString(created.Project.Name) != "orders" {
		t.Fatalf("CreateProject() = %#v, %v", created, err)
	}
	got, err := client.BatchGetProjects(ctx, &codebuild.BatchGetProjectsInput{Names: []string{"orders"}})
	if err != nil || len(got.Projects) != 1 || aws.ToString(got.Projects[0].Arn) == "" {
		t.Fatalf("BatchGetProjects() = %#v, %v", got, err)
	}
	listed, err := client.ListProjects(ctx, &codebuild.ListProjectsInput{})
	if err != nil || len(listed.Projects) != 1 || listed.Projects[0] != "orders" {
		t.Fatalf("ListProjects() = %#v, %v", listed, err)
	}
	if _, err := client.UpdateProject(ctx, &codebuild.UpdateProjectInput{Name: aws.String("orders"), Description: aws.String("updated")}); err != nil {
		t.Fatalf("UpdateProject() error = %v", err)
	}
	if _, err := client.DeleteProject(ctx, &codebuild.DeleteProjectInput{Name: aws.String("orders")}); err != nil {
		t.Fatalf("DeleteProject() error = %v", err)
	}
}

func TestCodeBuildCompatibilityAdapterRejectsCreateWithoutRequiredFields(t *testing.T) {
	server := httptest.NewServer(compataws.NewCodeBuildAdapter())
	defer server.Close()
	req, err := http.NewRequest(http.MethodPost, server.URL, strings.NewReader(`{"name":"orders"}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-Amz-Target", "CodeBuild_20161006.CreateProject")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusBadRequest || !strings.Contains(string(body), "InvalidInputException") {
		t.Fatalf("CreateProject(malformed) = %d %s", resp.StatusCode, body)
	}
}

func TestCodeBuildCompatibilityAdapterListsDescendingProjects(t *testing.T) {
	server := httptest.NewServer(compataws.NewCodeBuildAdapter())
	defer server.Close()
	client := codebuild.NewFromConfig(aws.Config{Region: "us-east-1", Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", "")}, func(o *codebuild.Options) { o.BaseEndpoint = aws.String(server.URL) })
	for _, name := range []string{"alpha", "bravo"} {
		if _, err := client.CreateProject(context.Background(), codeBuildProjectInput(name)); err != nil {
			t.Fatal(err)
		}
	}
	listed, err := client.ListProjects(context.Background(), &codebuild.ListProjectsInput{SortOrder: types.SortOrderTypeDescending})
	if err != nil || len(listed.Projects) != 2 || listed.Projects[0] != "bravo" {
		t.Fatalf("ListProjects(descending) = %#v, %v", listed, err)
	}
}

func TestCodeBuildCompatibilityAdapterRejectsUnsupportedListSort(t *testing.T) {
	server := httptest.NewServer(compataws.NewCodeBuildAdapter())
	defer server.Close()
	client := codebuild.NewFromConfig(aws.Config{Region: "us-east-1", Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", "")}, func(o *codebuild.Options) { o.BaseEndpoint = aws.String(server.URL) })
	_, err := client.ListProjects(context.Background(), &codebuild.ListProjectsInput{SortBy: types.ProjectSortByTypeCreatedTime})
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "InvalidInputException" {
		t.Fatalf("ListProjects(created time) error = %v, want InvalidInputException", err)
	}
}

func TestCodeBuildCompatibilityAdapterRetainsProjectConfiguration(t *testing.T) {
	server := httptest.NewServer(compataws.NewCodeBuildAdapter())
	defer server.Close()
	client := codebuild.NewFromConfig(aws.Config{Region: "us-east-1", Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", "")}, func(o *codebuild.Options) { o.BaseEndpoint = aws.String(server.URL) })
	input := codeBuildProjectInput("orders")
	input.Description = aws.String("initial")
	input.Tags = []types.Tag{{Key: aws.String("team"), Value: aws.String("platform")}}
	if _, err := client.CreateProject(context.Background(), input); err != nil {
		t.Fatal(err)
	}
	if _, err := client.UpdateProject(context.Background(), &codebuild.UpdateProjectInput{Name: aws.String("orders"), Description: aws.String("updated"), Source: &types.ProjectSource{Type: types.SourceTypeGithub, Location: aws.String("https://github.com/example/updated")}}); err != nil {
		t.Fatal(err)
	}
	got, err := client.BatchGetProjects(context.Background(), &codebuild.BatchGetProjectsInput{Names: []string{"orders"}})
	if err != nil || len(got.Projects) != 1 || aws.ToString(got.Projects[0].Description) != "updated" || aws.ToString(got.Projects[0].Source.Location) != "https://github.com/example/updated" || len(got.Projects[0].Tags) != 1 || aws.ToString(got.Projects[0].Tags[0].Key) != "team" || aws.ToString(got.Projects[0].Tags[0].Value) != "platform" {
		t.Fatalf("BatchGetProjects(updated) = %#v, %v", got, err)
	}
}

func TestCodeBuildCompatibilityAdapterAuthorizesEachBatchProject(t *testing.T) {
	ordersARN := "arn:aws:codebuild:us-east-1:000000000000:project/orders"
	auditLog := authz.NewAuditLog()
	server := httptest.NewServer(compataws.NewCodeBuildAdapter(compataws.WithCodeBuildAuthorizer(authz.NewPolicyAuthorizer(
		authz.Rule{Effect: authz.Allow, Actions: []string{"codebuild:CreateProject"}, Resources: []string{"*"}},
		authz.Rule{Effect: authz.Allow, Actions: []string{"codebuild:BatchGetProjects"}, Resources: []string{ordersARN}},
	)), compataws.WithCodeBuildAuditSink(auditLog.Record)))
	defer server.Close()
	client := codebuild.NewFromConfig(aws.Config{Region: "us-east-1", Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", "")}, func(o *codebuild.Options) { o.BaseEndpoint = aws.String(server.URL) })
	if _, err := client.CreateProject(context.Background(), codeBuildProjectInput("orders")); err != nil {
		t.Fatal(err)
	}
	projects, err := client.BatchGetProjects(context.Background(), &codebuild.BatchGetProjectsInput{Names: []string{"orders"}})
	if err != nil || len(projects.Projects) != 1 {
		t.Fatalf("BatchGetProjects(orders) = %#v, %v", projects, err)
	}
	_, err = client.BatchGetProjects(context.Background(), &codebuild.BatchGetProjectsInput{Names: []string{"orders", "secret"}})
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "AccessDeniedException" {
		t.Fatalf("BatchGetProjects(with secret) error = %v, want AccessDeniedException", err)
	}
	assertDecision(t, auditLog.Decisions(), "codebuild:BatchGetProjects", false)
}

func TestCodeBuildCompatibilityAdapterSurfacesAuthorizerFailures(t *testing.T) {
	server := httptest.NewServer(compataws.NewCodeBuildAdapter(compataws.WithCodeBuildAuthorizer(authz.AuthorizerFunc(func(context.Context, authz.Request) (authz.Decision, error) {
		return authz.Decision{}, errors.New("authorizer unavailable")
	}))))
	defer server.Close()
	client := codebuild.NewFromConfig(aws.Config{Region: "us-east-1", Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", "")}, func(o *codebuild.Options) { o.BaseEndpoint = aws.String(server.URL) })
	_, err := client.CreateProject(context.Background(), codeBuildProjectInput("orders"))
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "InternalError" {
		t.Fatalf("CreateProject(authorizer failure) error = %v, want InternalError", err)
	}
}

func codeBuildProjectInput(name string) *codebuild.CreateProjectInput {
	return &codebuild.CreateProjectInput{Name: aws.String(name), ServiceRole: aws.String("arn:aws:iam::000000000000:role/codebuild"), Artifacts: &types.ProjectArtifacts{Type: types.ArtifactsTypeNoArtifacts}, Environment: &types.ProjectEnvironment{ComputeType: types.ComputeTypeBuildGeneral1Small, Image: aws.String("aws/codebuild/standard:7.0"), Type: types.EnvironmentTypeLinuxContainer}, Source: &types.ProjectSource{Type: types.SourceTypeGithub, Location: aws.String("https://github.com/example/orders")}}
}

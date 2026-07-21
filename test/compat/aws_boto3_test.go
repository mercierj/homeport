package compat_test

import (
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"testing"

	compataws "github.com/homeport/homeport/internal/app/compat/aws"
)

func TestDynamoDBCompatibilityAdapterWithBoto3EndpointOverride(t *testing.T) {
	server := httptest.NewServer(compataws.NewDynamoDBAdapter())
	defer server.Close()

	runBoto3(t, server.URL, `
import boto3

client = boto3.client("dynamodb", endpoint_url=endpoint, region_name="us-east-1")
created = client.create_table(
    TableName="boto3-things",
    AttributeDefinitions=[{"AttributeName": "id", "AttributeType": "S"}],
    KeySchema=[{"AttributeName": "id", "KeyType": "HASH"}],
    BillingMode="PAY_PER_REQUEST",
)
assert created["TableDescription"]["TableName"] == "boto3-things"
assert created["TableDescription"]["TableStatus"] == "ACTIVE"

client.put_item(TableName="boto3-things", Item={"id": {"S": "1"}, "value": {"S": "hello"}})
got = client.get_item(TableName="boto3-things", Key={"id": {"S": "1"}})
assert got["Item"]["value"]["S"] == "hello"

queried = client.query(
    TableName="boto3-things",
    KeyConditionExpression="id = :id",
    ExpressionAttributeValues={":id": {"S": "1"}},
)
assert queried["Items"][0]["value"]["S"] == "hello"

client.delete_table(TableName="boto3-things")
`)
}

func TestACMCompatibilityAdapterWithBoto3EndpointOverride(t *testing.T) {
	server := httptest.NewServer(compataws.NewACMAdapter())
	defer server.Close()

	runBoto3(t, server.URL, `
import boto3

client = boto3.client("acm", endpoint_url=endpoint, region_name="us-east-1")
requested = client.request_certificate(DomainName="boto3.example.test")
arn = requested["CertificateArn"]
assert arn

described = client.describe_certificate(CertificateArn=arn)
cert = described["Certificate"]
assert cert["CertificateArn"] == arn
assert cert["DomainName"] == "boto3.example.test"
assert cert["Status"] == "ISSUED"

listed = client.list_certificates()
assert listed["CertificateSummaryList"][0]["CertificateArn"] == arn

client.delete_certificate(CertificateArn=arn)
assert client.list_certificates()["CertificateSummaryList"] == []
`)
}

func TestAPIGatewayCompatibilityAdapterWithBoto3EndpointOverride(t *testing.T) {
	server := httptest.NewServer(compataws.NewAPIGatewayAdapter())
	defer server.Close()

	runBoto3(t, server.URL, `
import boto3

client = boto3.client("apigateway", endpoint_url=endpoint, region_name="us-east-1")
created = client.create_rest_api(name="boto3-orders-api", description="orders")
api_id = created["id"]
assert api_id
assert created["name"] == "boto3-orders-api"

got = client.get_rest_api(restApiId=api_id)
assert got["id"] == api_id
assert got["name"] == "boto3-orders-api"

listed = client.get_rest_apis()
assert listed["items"][0]["id"] == api_id

updated = client.update_rest_api(
    restApiId=api_id,
    patchOperations=[{"op": "replace", "path": "/name", "value": "boto3-orders-api-v2"}],
)
assert updated["name"] == "boto3-orders-api-v2"

client.delete_rest_api(restApiId=api_id)
assert client.get_rest_apis()["items"] == []
`)
}

func TestCloudWatchLogsCompatibilityAdapterWithBoto3EndpointOverride(t *testing.T) {
	server := httptest.NewServer(compataws.NewCloudWatchLogsAdapter())
	defer server.Close()

	runBoto3(t, server.URL, `
import boto3

client = boto3.client("logs", endpoint_url=endpoint, region_name="us-east-1")
client.create_log_group(logGroupName="/boto3/app", tags={"env": "test"})
client.create_log_stream(logGroupName="/boto3/app", logStreamName="web")
put = client.put_log_events(
    logGroupName="/boto3/app",
    logStreamName="web",
    logEvents=[{"timestamp": 1, "message": "hello"}],
)
assert put["nextSequenceToken"] == "1"

streams = client.describe_log_streams(logGroupName="/boto3/app")
assert streams["logStreams"][0]["logStreamName"] == "web"

events = client.get_log_events(logGroupName="/boto3/app", logStreamName="web")
assert events["events"][0]["message"] == "hello"

client.delete_log_stream(logGroupName="/boto3/app", logStreamName="web")
client.delete_log_group(logGroupName="/boto3/app")
assert client.describe_log_groups(logGroupNamePrefix="/boto3")["logGroups"] == []
`)
}

func TestECSCompatibilityAdapterWithBoto3EndpointOverride(t *testing.T) {
	server := httptest.NewServer(compataws.NewECSAdapter())
	defer server.Close()

	runBoto3(t, server.URL, `
import boto3

client = boto3.client("ecs", endpoint_url=endpoint, region_name="us-east-1")
created = client.create_service(
    cluster="default",
    serviceName="boto3-web",
    taskDefinition="web:1",
    desiredCount=2,
    launchType="EXTERNAL",
)
service = created["service"]
assert service["serviceName"] == "boto3-web"
assert service["status"] == "ACTIVE"
assert service["desiredCount"] == 2

described = client.describe_services(cluster="default", services=["boto3-web"])
assert described["services"][0]["serviceArn"] == service["serviceArn"]

listed = client.list_services(cluster="default")
assert listed["serviceArns"] == [service["serviceArn"]]

updated = client.update_service(cluster="default", service="boto3-web", desiredCount=3)
assert updated["service"]["desiredCount"] == 3

client.delete_service(cluster="default", service="boto3-web", force=True)
listed = client.list_services(cluster="default")
assert listed["serviceArns"] == []
`)
}

func TestEventBridgeCompatibilityAdapterWithBoto3EndpointOverride(t *testing.T) {
	server := httptest.NewServer(compataws.NewEventBridgeAdapter())
	defer server.Close()

	runBoto3(t, server.URL, `
import boto3

client = boto3.client("events", endpoint_url=endpoint, region_name="us-east-1")
rule = client.put_rule(Name="boto3-orders-created", EventPattern='{"source":["orders"]}', State="ENABLED")
assert rule["RuleArn"]

listed = client.list_rules()
assert listed["Rules"][0]["Name"] == "boto3-orders-created"

put = client.put_events(Entries=[{"Source": "orders", "DetailType": "created", "Detail": '{"id":"1"}'}])
assert put["FailedEntryCount"] == 0
assert put["Entries"][0]["EventId"]

client.delete_rule(Name="boto3-orders-created")
assert client.list_rules()["Rules"] == []
`)
}

func TestEFSCompatibilityAdapterWithBoto3EndpointOverride(t *testing.T) {
	server := httptest.NewServer(compataws.NewEFSAdapter())
	defer server.Close()

	runBoto3(t, server.URL, `
import boto3

client = boto3.client("efs", endpoint_url=endpoint, region_name="us-east-1")
created = client.create_file_system(CreationToken="boto3-orders-fs", Tags=[{"Key": "Name", "Value": "orders"}])
fs_id = created["FileSystemId"]
assert fs_id
assert created["CreationToken"] == "boto3-orders-fs"
assert created["LifeCycleState"] == "available"

described = client.describe_file_systems(FileSystemId=fs_id)
assert described["FileSystems"][0]["FileSystemId"] == fs_id

updated = client.update_file_system(FileSystemId=fs_id, ThroughputMode="elastic")
assert updated["ThroughputMode"] == "elastic"

client.delete_file_system(FileSystemId=fs_id)
assert client.describe_file_systems()["FileSystems"] == []
`)
}

func TestEKSCompatibilityAdapterWithBoto3EndpointOverride(t *testing.T) {
	server := httptest.NewServer(compataws.NewEKSAdapter())
	defer server.Close()

	runBoto3(t, server.URL, `
import boto3

client = boto3.client("eks", endpoint_url=endpoint, region_name="us-east-1")
created = client.create_cluster(
    name="boto3-orders",
    roleArn="arn:aws:iam::000000000000:role/homeport-eks",
    resourcesVpcConfig={"subnetIds": ["subnet-a", "subnet-b"]},
    tags={"env": "test"},
)
cluster = created["cluster"]
assert cluster["name"] == "boto3-orders"
assert cluster["status"] == "ACTIVE"

described = client.describe_cluster(name="boto3-orders")
assert described["cluster"]["arn"] == cluster["arn"]

listed = client.list_clusters()
assert listed["clusters"] == ["boto3-orders"]

updated = client.update_cluster_config(name="boto3-orders", resourcesVpcConfig={"endpointPublicAccess": False, "endpointPrivateAccess": True})
assert updated["update"]["status"] == "Successful"

deleted = client.delete_cluster(name="boto3-orders")
assert deleted["cluster"]["name"] == "boto3-orders"
assert client.list_clusters()["clusters"] == []
`)
}

func TestIAMCompatibilityAdapterWithBoto3EndpointOverride(t *testing.T) {
	server := httptest.NewServer(compataws.NewIAMAdapter())
	defer server.Close()

	runBoto3(t, server.URL, `
import boto3

policy = '{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Principal":{"Service":"lambda.amazonaws.com"},"Action":"sts:AssumeRole"}]}'
client = boto3.client("iam", endpoint_url=endpoint, region_name="us-east-1")
created = client.create_role(
    RoleName="boto3-homeport-orders",
    AssumeRolePolicyDocument=policy,
    Description="initial role",
    Tags=[{"Key": "env", "Value": "test"}],
)
role = created["Role"]
assert role["RoleName"] == "boto3-homeport-orders"
assert role["Arn"]

got = client.get_role(RoleName="boto3-homeport-orders")
assert got["Role"]["Arn"] == role["Arn"]

listed = client.list_roles()
assert listed["Roles"][0]["RoleName"] == "boto3-homeport-orders"

client.update_role(RoleName="boto3-homeport-orders", Description="updated role")
got = client.get_role(RoleName="boto3-homeport-orders")
assert got["Role"]["Description"] == "updated role"

client.delete_role(RoleName="boto3-homeport-orders")
assert client.list_roles()["Roles"] == []
`)
}

func TestKinesisCompatibilityAdapterWithBoto3EndpointOverride(t *testing.T) {
	server := httptest.NewServer(compataws.NewKinesisAdapter())
	defer server.Close()

	runBoto3(t, server.URL, `
import boto3

client = boto3.client("kinesis", endpoint_url=endpoint, region_name="us-east-1")
client.create_stream(StreamName="boto3-events", ShardCount=1)

listed = client.list_streams()
assert listed["StreamNames"] == ["boto3-events"]

put = client.put_record(StreamName="boto3-events", PartitionKey="orders", Data=b"hello")
assert put["ShardId"] == "shardId-000000000000"

iterator = client.get_shard_iterator(
    StreamName="boto3-events",
    ShardId="shardId-000000000000",
    ShardIteratorType="TRIM_HORIZON",
)["ShardIterator"]
records = client.get_records(ShardIterator=iterator)
assert records["Records"][0]["Data"] == b"hello"

client.delete_stream(StreamName="boto3-events")
assert client.list_streams()["StreamNames"] == []
`)
}

func TestKMSCompatibilityAdapterWithBoto3EndpointOverride(t *testing.T) {
	server := httptest.NewServer(compataws.NewKMSAdapter())
	defer server.Close()

	runBoto3(t, server.URL, `
import boto3

client = boto3.client("kms", endpoint_url=endpoint, region_name="us-east-1")
created = client.create_key(Description="boto3 key", Tags=[{"TagKey": "env", "TagValue": "test"}])
metadata = created["KeyMetadata"]
key_id = metadata["KeyId"]
assert key_id
assert metadata["Description"] == "boto3 key"

described = client.describe_key(KeyId=key_id)
assert described["KeyMetadata"]["Arn"] == metadata["Arn"]

keys = client.list_keys()
assert keys["Keys"][0]["KeyId"] == key_id

client.schedule_key_deletion(KeyId=key_id, PendingWindowInDays=7)
described = client.describe_key(KeyId=key_id)
assert described["KeyMetadata"]["KeyState"] == "PendingDeletion"
`)
}

func TestLambdaCompatibilityAdapterWithBoto3EndpointOverride(t *testing.T) {
	server := httptest.NewServer(compataws.NewLambdaAdapter())
	defer server.Close()

	runBoto3(t, server.URL, `
import boto3

client = boto3.client("lambda", endpoint_url=endpoint, region_name="us-east-1")
created = client.create_function(
    FunctionName="boto3-orders-handler",
    Runtime="nodejs20.x",
    Role="arn:aws:iam::000000000000:role/homeport",
    Handler="index.handler",
    Code={"ZipFile": b"fake zip"},
)
assert created["FunctionName"] == "boto3-orders-handler"

got = client.get_function(FunctionName="boto3-orders-handler")
assert got["Configuration"]["Handler"] == "index.handler"

updated = client.update_function_code(FunctionName="boto3-orders-handler", ZipFile=b"new fake zip")
assert updated["FunctionName"] == "boto3-orders-handler"
assert updated["RevisionId"] != created["RevisionId"]

invoked = client.invoke(FunctionName="boto3-orders-handler", Payload=b'{"hello":"world"}')
assert invoked["StatusCode"] == 200
assert b'"function":"boto3-orders-handler"' in invoked["Payload"].read()

client.delete_function(FunctionName="boto3-orders-handler")
`)
}

func TestCognitoCompatibilityAdapterWithBoto3EndpointOverride(t *testing.T) {
	server := httptest.NewServer(compataws.NewCognitoAdapter())
	defer server.Close()

	runBoto3(t, server.URL, `
import boto3

client = boto3.client("cognito-idp", endpoint_url=endpoint, region_name="us-east-1")
created = client.create_user_pool(PoolName="boto3-customers")
pool = created["UserPool"]
pool_id = pool["Id"]
assert pool_id
assert pool["Name"] == "boto3-customers"

described = client.describe_user_pool(UserPoolId=pool_id)
assert described["UserPool"]["Name"] == "boto3-customers"

listed = client.list_user_pools(MaxResults=10)
assert listed["UserPools"][0]["Id"] == pool_id

client.update_user_pool(UserPoolId=pool_id, MfaConfiguration="OPTIONAL")
described = client.describe_user_pool(UserPoolId=pool_id)
assert described["UserPool"]["MfaConfiguration"] == "OPTIONAL"

client.delete_user_pool(UserPoolId=pool_id)
`)
}

func TestSecretsManagerCompatibilityAdapterWithBoto3EndpointOverride(t *testing.T) {
	server := httptest.NewServer(compataws.NewSecretsAdapter())
	defer server.Close()

	runBoto3(t, server.URL, `
import boto3

client = boto3.client("secretsmanager", endpoint_url=endpoint, region_name="us-east-1")
created = client.create_secret(Name="boto3/app/db", SecretString="first", Tags=[{"Key": "env", "Value": "test"}])
assert created["Name"] == "boto3/app/db"

updated = client.put_secret_value(SecretId="boto3/app/db", SecretString="second")
assert updated["VersionId"] == "2"

got = client.get_secret_value(SecretId="boto3/app/db")
assert got["SecretString"] == "second"

desc = client.describe_secret(SecretId="boto3/app/db")
assert desc["Name"] == "boto3/app/db"

listed = client.list_secrets()
assert listed["SecretList"][0]["Name"] == "boto3/app/db"

client.delete_secret(SecretId="boto3/app/db", ForceDeleteWithoutRecovery=True)
`)
}

func TestSESCompatibilityAdapterWithBoto3EndpointOverride(t *testing.T) {
	server := httptest.NewServer(compataws.NewSESAdapter())
	defer server.Close()

	runBoto3(t, server.URL, `
import boto3

client = boto3.client("ses", endpoint_url=endpoint, region_name="us-east-1")
created = client.verify_domain_identity(Domain="boto3.example.com")
token = created["VerificationToken"]
assert token

attrs = client.get_identity_verification_attributes(Identities=["boto3.example.com"])
identity = attrs["VerificationAttributes"]["boto3.example.com"]
assert identity["VerificationToken"] == token
assert identity["VerificationStatus"] == "Pending"

listed = client.list_identities(IdentityType="Domain")
assert listed["Identities"] == ["boto3.example.com"]

client.delete_identity(Identity="boto3.example.com")
assert client.list_identities()["Identities"] == []
`)
}

func TestSNSCompatibilityAdapterWithBoto3EndpointOverride(t *testing.T) {
	server := httptest.NewServer(compataws.NewSNSAdapter())
	defer server.Close()

	runBoto3(t, server.URL, `
import boto3

client = boto3.client("sns", endpoint_url=endpoint, region_name="us-east-1")
created = client.create_topic(Name="boto3-events", Tags=[{"Key": "env", "Value": "test"}])
topic_arn = created["TopicArn"]
assert topic_arn

attrs = client.get_topic_attributes(TopicArn=topic_arn)
assert attrs["Attributes"]["TopicArn"] == topic_arn

listed = client.list_topics()
assert listed["Topics"][0]["TopicArn"] == topic_arn

client.publish(TopicArn=topic_arn, Message="hello")
client.delete_topic(TopicArn=topic_arn)
assert client.list_topics()["Topics"] == []
`)
}

func TestSQSCompatibilityAdapterWithBoto3EndpointOverride(t *testing.T) {
	server := httptest.NewServer(compataws.NewSQSAdapter())
	defer server.Close()

	runBoto3(t, server.URL, `
import boto3

client = boto3.client("sqs", endpoint_url=endpoint, region_name="us-east-1")
created = client.create_queue(QueueName="boto3-jobs", tags={"env": "test"})
queue_url = created["QueueUrl"]
assert queue_url

got = client.get_queue_url(QueueName="boto3-jobs")
assert got["QueueUrl"] == queue_url

sent = client.send_message(QueueUrl=queue_url, MessageBody="hello")
assert sent["MessageId"]

received = client.receive_message(QueueUrl=queue_url)
message = received["Messages"][0]
assert message["Body"] == "hello"

client.delete_message(QueueUrl=queue_url, ReceiptHandle=message["ReceiptHandle"])
client.delete_queue(QueueUrl=queue_url)
assert client.list_queues().get("QueueUrls", []) == []
`)
}

func TestS3CompatibilityAdapterWithBoto3EndpointOverride(t *testing.T) {
	server := httptest.NewServer(compataws.NewS3Adapter())
	defer server.Close()

	runBoto3(t, server.URL, `
import boto3
from botocore.config import Config

client = boto3.client(
    "s3",
    endpoint_url=endpoint,
    region_name="us-east-1",
    config=Config(s3={"addressing_style": "path"}),
)
client.create_bucket(Bucket="boto3-bucket")
client.put_object(Bucket="boto3-bucket", Key="hello.txt", Body=b"hello")
got = client.get_object(Bucket="boto3-bucket", Key="hello.txt")
assert got["Body"].read() == b"hello"
client.delete_object(Bucket="boto3-bucket", Key="hello.txt")
client.delete_bucket(Bucket="boto3-bucket")
`)
}

func runBoto3(t *testing.T, endpoint, script string) {
	t.Helper()
	python := boto3Python(t)
	wrapped := "endpoint = " + pythonString(endpoint) + "\n" + script
	cmd := exec.Command(python, "-c", wrapped)
	cmd.Env = append(os.Environ(),
		"AWS_ACCESS_KEY_ID=homeport",
		"AWS_SECRET_ACCESS_KEY=homeport",
		"AWS_EC2_METADATA_DISABLED=true",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("boto3 script failed: %v\n%s", err, out)
	}
}

func boto3Python(t *testing.T) string {
	t.Helper()
	python := os.Getenv("BOTO3_PYTHON")
	if python == "" {
		python = "python3"
	}
	cmd := exec.Command(python, "-c", "import boto3")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("boto3 not installed for %s; set BOTO3_PYTHON to a Python with boto3: %v\n%s", python, err, out)
	}
	return python
}

func pythonString(value string) string {
	return `"` + strings.ReplaceAll(value, `"`, `\"`) + `"`
}

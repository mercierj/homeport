| PROVIDER | SERVICE | STATUS | RESOURCES |
| --- | --- | --- | --- |
| aws | ALB | mapped | aws_lb |
| aws | ACM | mapped | aws_acm_certificate |
| aws | API Gateway | mapped | aws_api_gateway_rest_api |
| aws | AppSync | missing |  |
| aws | Athena | missing |  |
| aws | Bedrock | missing |  |
| aws | CloudFront | mapped | aws_cloudfront_distribution |
| aws | CloudWatch | mapped | aws_cloudwatch_metric_alarm, aws_cloudwatch_log_group, aws_cloudwatch_dashboard |
| aws | CodeBuild | missing |  |
| aws | CodePipeline | missing |  |
| aws | Cognito | guided | aws_cognito_user_pool |
| aws | DynamoDB | mapped | aws_dynamodb_table |
| aws | EBS | mapped | aws_ebs_volume |
| aws | EC2 | mapped | aws_instance |
| aws | ECR | missing |  |
| aws | ECS | mapped | aws_ecs_service, aws_ecs_task_definition |
| aws | EFS | mapped | aws_efs_file_system |
| aws | EKS | mapped | aws_eks_cluster |
| aws | EMR | missing |  |
| aws | ElastiCache | mapped | aws_elasticache_cluster |
| aws | EventBridge | mapped | aws_cloudwatch_event_rule |
| aws | Glue | missing |  |
| aws | GuardDuty | missing |  |
| aws | IAM | mapped | aws_iam_role |
| aws | KMS | guided | aws_kms_key |
| aws | Kinesis | mapped | aws_kinesis_stream |
| aws | Lambda | mapped | aws_lambda_function |
| aws | MSK | missing |  |
| aws | OpenSearch | missing |  |
| aws | RDS | mapped | aws_db_instance, aws_rds_cluster |
| aws | Redshift | missing |  |
| aws | Route 53 | mapped | aws_route53_zone |
| aws | S3 | mapped | aws_s3_bucket |
| aws | SES | mapped | aws_ses_domain_identity |
| aws | SNS | mapped | aws_sns_topic |
| aws | SQS | mapped | aws_sqs_queue |
| aws | SageMaker | missing |  |
| aws | Secrets Manager | mapped | aws_secretsmanager_secret |
| aws | Step Functions | missing |  |
| aws | VPC | mapped | aws_vpc |
| aws | WAF | missing |  |
| aws | X-Ray | missing |  |
| gcp | Apigee | missing |  |
| gcp | App Engine | mapped | google_app_engine_application |
| gcp | Artifact Registry | missing |  |
| gcp | BigQuery | missing |  |
| gcp | Bigtable | guided | google_bigtable_instance |
| gcp | Cloud Armor | mapped | google_compute_security_policy |
| gcp | Cloud Build | missing |  |
| gcp | Cloud CDN | mapped | google_compute_backend_bucket |
| gcp | Cloud DNS | mapped | google_dns_managed_zone |
| gcp | Cloud Functions | mapped | google_cloudfunctions_function |
| gcp | Cloud Load Balancing | mapped | google_compute_backend_service |
| gcp | Cloud Run | mapped | google_cloud_run_service |
| gcp | Cloud Scheduler | mapped | google_cloud_scheduler_job |
| gcp | Cloud SQL | mapped | google_sql_database_instance |
| gcp | Cloud Storage | guided | google_storage_bucket |
| gcp | Cloud Tasks | mapped | google_cloud_tasks_queue |
| gcp | Compute Engine | mapped | google_compute_instance |
| gcp | Composer | missing |  |
| gcp | Dataflow | missing |  |
| gcp | Dataproc | missing |  |
| gcp | Eventarc | missing |  |
| gcp | Filestore | mapped | google_filestore_instance |
| gcp | Firestore | guided | google_firestore_database |
| gcp | GKE | mapped | google_container_cluster |
| gcp | IAM | mapped | google_project_iam_member |
| gcp | Identity Platform | mapped | google_identity_platform_config |
| gcp | Logging | missing |  |
| gcp | Memorystore | mapped | google_redis_instance |
| gcp | Monitoring | missing |  |
| gcp | Persistent Disk | mapped | google_compute_disk |
| gcp | Pub/Sub | mapped | google_pubsub_topic, google_pubsub_subscription |
| gcp | Secret Manager | mapped | google_secret_manager_secret |
| gcp | Spanner | guided | google_spanner_instance |
| gcp | Trace | missing |  |
| gcp | VPC | mapped | google_compute_network |
| gcp | Vertex AI | missing |  |
| gcp | Workflows | missing |  |
| azure | AI Search | missing |  |
| azure | AKS | mapped | azurerm_kubernetes_cluster |
| azure | API Management | missing |  |
| azure | App Gateway | mapped | azurerm_application_gateway |
| azure | App Insights | missing |  |
| azure | App Service | mapped | azurerm_app_service |
| azure | Azure AD B2C | mapped | azurerm_aadb2c_directory |
| azure | Azure Cache | mapped | azurerm_redis_cache |
| azure | Azure CDN | mapped | azurerm_cdn_profile |
| azure | Azure DNS | mapped | azurerm_dns_zone |
| azure | Azure Firewall | mapped | azurerm_firewall |
| azure | Azure Functions | mapped | azurerm_function_app |
| azure | Azure Load Balancer | mapped | azurerm_lb |
| azure | Azure SQL | mapped | azurerm_mssql_database |
| azure | Azure Storage | guided | azurerm_storage_container, azurerm_storage_account, azurerm_storage_share |
| azure | Azure VNet | mapped | azurerm_virtual_network |
| azure | Azure VM | mapped | azurerm_linux_virtual_machine, azurerm_windows_virtual_machine |
| azure | Container Apps | missing |  |
| azure | Container Instances | mapped | azurerm_container_group |
| azure | Container Registry | missing |  |
| azure | Cosmos DB | guided | azurerm_cosmosdb_account |
| azure | Data Factory | missing |  |
| azure | Databricks | missing |  |
| azure | Event Grid | mapped | azurerm_eventgrid_topic |
| azure | Event Hubs | mapped | azurerm_eventhub |
| azure | Foundry/OpenAI | missing |  |
| azure | Front Door | mapped | azurerm_frontdoor |
| azure | IoT Hub | missing |  |
| azure | Key Vault | mapped | azurerm_key_vault |
| azure | Log Analytics | missing |  |
| azure | Logic Apps | mapped | azurerm_logic_app_workflow |
| azure | Managed Disk | mapped | azurerm_managed_disk |
| azure | Monitor | missing |  |
| azure | MySQL | mapped | azurerm_mysql_flexible_server |
| azure | PostgreSQL | mapped | azurerm_postgresql_flexible_server |
| azure | Service Bus | mapped | azurerm_servicebus_namespace, azurerm_servicebus_queue |
| azure | SignalR | missing |  |
| azure | Synapse | missing |  |
| azure | VM Scale Sets | missing |  |

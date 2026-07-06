| PROVIDER | SERVICE | STATUS | RESOURCES |
| --- | --- | --- | --- |
| aws | ALB | full | aws_lb |
| aws | ACM | full | aws_acm_certificate |
| aws | API Gateway | full | aws_api_gateway_rest_api |
| aws | AppSync | full |  |
| aws | Athena | full |  |
| aws | Bedrock | full |  |
| aws | CloudFront | full | aws_cloudfront_distribution |
| aws | CloudWatch | full | aws_cloudwatch_metric_alarm, aws_cloudwatch_log_group, aws_cloudwatch_dashboard |
| aws | CodeBuild | full |  |
| aws | CodePipeline | full |  |
| aws | Cognito | full | aws_cognito_user_pool |
| aws | DynamoDB | full | aws_dynamodb_table |
| aws | EBS | full | aws_ebs_volume |
| aws | EC2 | full | aws_instance |
| aws | ECR | full |  |
| aws | ECS | full | aws_ecs_service, aws_ecs_task_definition |
| aws | EFS | full | aws_efs_file_system |
| aws | EKS | full | aws_eks_cluster |
| aws | EMR | full |  |
| aws | ElastiCache | full | aws_elasticache_cluster |
| aws | EventBridge | full | aws_cloudwatch_event_rule |
| aws | Glue | full |  |
| aws | GuardDuty | full |  |
| aws | IAM | full | aws_iam_role |
| aws | KMS | full | aws_kms_key |
| aws | Kinesis | full | aws_kinesis_stream |
| aws | Lambda | full | aws_lambda_function |
| aws | MSK | full |  |
| aws | OpenSearch | full |  |
| aws | RDS | full | aws_db_instance, aws_rds_cluster |
| aws | Redshift | full |  |
| aws | Route 53 | full | aws_route53_zone |
| aws | S3 | full | aws_s3_bucket |
| aws | SES | full | aws_ses_domain_identity |
| aws | SNS | full | aws_sns_topic |
| aws | SQS | full | aws_sqs_queue |
| aws | SageMaker | full |  |
| aws | Secrets Manager | full | aws_secretsmanager_secret |
| aws | Step Functions | full |  |
| aws | VPC | full | aws_vpc |
| aws | WAF | full |  |
| aws | X-Ray | full |  |
| aws | Lake Formation | full | aws_lakeformation_data_lake_settings, aws_lakeformation_permissions |
| aws | QuickSight | full | aws_quicksight_data_source, aws_quicksight_dashboard |
| aws | MQ | full | aws_mq_broker |
| aws | IoT Core | full | aws_iot_thing, aws_iot_topic_rule |
| aws | App Mesh | full | aws_appmesh_mesh, aws_appmesh_virtual_node |
| aws | CodeDeploy | full | aws_codedeploy_app, aws_codedeploy_deployment_group |
| aws | CloudFormation full import | full | aws_cloudformation_stack |
| aws | Shield | full | aws_shield_protection |
| aws | Security Hub | full | aws_securityhub_account, aws_securityhub_standards_subscription |
| aws | Config | full | aws_config_configuration_recorder, aws_config_config_rule |
| aws | Organizations | full | aws_organizations_organization, aws_organizations_account |
| aws | Control Tower | full | aws_controltower_control |
| aws | Textract | full | aws_textract_adapter |
| aws | Transcribe | full | aws_transcribe_vocabulary, aws_transcribe_language_model |
| aws | Translate | full |  |
| aws | Rekognition | full | aws_rekognition_collection, aws_rekognition_project |
| aws | Comprehend | full | aws_comprehend_document_classifier, aws_comprehend_entity_recognizer |
| gcp | Apigee | full |  |
| gcp | App Engine | full | google_app_engine_application |
| gcp | Artifact Registry | full |  |
| gcp | BigQuery | full |  |
| gcp | Bigtable | full | google_bigtable_instance |
| gcp | Cloud Armor | full | google_compute_security_policy |
| gcp | Cloud Build | full |  |
| gcp | Cloud CDN | full | google_compute_backend_bucket |
| gcp | Cloud DNS | full | google_dns_managed_zone |
| gcp | Cloud Functions | full | google_cloudfunctions_function |
| gcp | Cloud Load Balancing | full | google_compute_backend_service |
| gcp | Cloud Run | full | google_cloud_run_service |
| gcp | Cloud Scheduler | full | google_cloud_scheduler_job |
| gcp | Cloud SQL | full | google_sql_database_instance |
| gcp | Cloud Storage | full | google_storage_bucket |
| gcp | Cloud Tasks | full | google_cloud_tasks_queue |
| gcp | Compute Engine | full | google_compute_instance |
| gcp | Composer | full |  |
| gcp | Dataflow | full |  |
| gcp | Dataproc | full |  |
| gcp | Eventarc | full |  |
| gcp | Filestore | full | google_filestore_instance |
| gcp | Firestore | full | google_firestore_database |
| gcp | GKE | full | google_container_cluster |
| gcp | IAM | full | google_project_iam_member |
| gcp | Identity Platform | full | google_identity_platform_config |
| gcp | Logging | full |  |
| gcp | Memorystore | full | google_redis_instance |
| gcp | Monitoring | full |  |
| gcp | Persistent Disk | full | google_compute_disk |
| gcp | Pub/Sub | full | google_pubsub_topic, google_pubsub_subscription |
| gcp | Secret Manager | full | google_secret_manager_secret |
| gcp | Spanner | full | google_spanner_instance |
| gcp | Trace | full |  |
| gcp | VPC | full | google_compute_network |
| gcp | Vertex AI | full |  |
| gcp | Workflows | full |  |
| gcp | Dataplex | full | google_dataplex_lake, google_dataplex_zone |
| gcp | Looker | full | google_looker_instance |
| gcp | Cloud Deploy | full | google_clouddeploy_delivery_pipeline, google_clouddeploy_target |
| gcp | Error Reporting | full |  |
| gcp | Profiler | full |  |
| gcp | TPU | full | google_tpu_node, google_tpu_v2_vm |
| gcp | Document AI | full | google_document_ai_processor |
| gcp | Vision AI | full |  |
| gcp | Speech-to-Text | full | google_speech_custom_class, google_speech_phrase_set |
| gcp | Translation | full |  |
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
| azure | Event Grid | guided | azurerm_eventgrid_topic |
| azure | Event Hubs | guided | azurerm_eventhub |
| azure | Foundry/OpenAI | missing |  |
| azure | Front Door | mapped | azurerm_frontdoor |
| azure | IoT Hub | missing |  |
| azure | Key Vault | mapped | azurerm_key_vault |
| azure | Log Analytics | missing |  |
| azure | Logic Apps | guided | azurerm_logic_app_workflow |
| azure | Managed Disk | mapped | azurerm_managed_disk |
| azure | Monitor | missing |  |
| azure | MySQL | mapped | azurerm_mysql_flexible_server |
| azure | PostgreSQL | mapped | azurerm_postgresql_flexible_server |
| azure | Service Bus | guided | azurerm_servicebus_namespace, azurerm_servicebus_queue |
| azure | SignalR | missing |  |
| azure | Synapse | missing |  |
| azure | VM Scale Sets | missing |  |
| azure | Data Lake | missing | azurerm_storage_data_lake_gen2_filesystem |
| azure | Fabric | missing |  |
| azure | Power BI Embedded | missing | azurerm_powerbi_embedded_capacity |
| azure | Logic Apps advanced | missing | azurerm_logic_app_workflow, azurerm_logic_app_trigger_http_request |
| azure | Notification Hubs | missing | azurerm_notification_hub_namespace, azurerm_notification_hub |
| azure | DevOps Pipelines | missing | azuredevops_build_definition, azuredevops_release_definition |
| azure | Application Insights | missing | azurerm_application_insights |
| azure | Automation | missing | azurerm_automation_account, azurerm_automation_runbook |
| azure | Purview | missing | azurerm_purview_account |
| azure | Machine Learning | missing | azurerm_machine_learning_workspace |
| azure | Document Intelligence | missing | azurerm_cognitive_account |
| azure | Speech | missing | azurerm_cognitive_account |
| azure | Translator | missing | azurerm_cognitive_account |

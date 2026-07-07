| PROVIDER | SERVICE | STATUS | RESOURCES |
| --- | --- | --- | --- |
| aws | ALB | full | aws_lb |
| aws | ACM | full | aws_acm_certificate |
| aws | API Gateway | full | aws_api_gateway_rest_api |
| aws | AppSync | full | aws_appsync_graphql_api |
| aws | Athena | full | aws_athena_workgroup |
| aws | Bedrock | full | aws_bedrock_inference_profile |
| aws | CloudFront | full | aws_cloudfront_distribution |
| aws | CloudWatch | full | aws_cloudwatch_metric_alarm, aws_cloudwatch_log_group, aws_cloudwatch_dashboard |
| aws | CodeBuild | full | aws_codebuild_project |
| aws | CodePipeline | full | aws_codepipeline |
| aws | Cognito | full | aws_cognito_user_pool |
| aws | DynamoDB | full | aws_dynamodb_table |
| aws | EBS | full | aws_ebs_volume |
| aws | EC2 | full | aws_instance |
| aws | ECR | full | aws_ecr_repository |
| aws | ECS | full | aws_ecs_service, aws_ecs_task_definition |
| aws | EFS | full | aws_efs_file_system |
| aws | EKS | full | aws_eks_cluster |
| aws | EMR | full | aws_emr_cluster |
| aws | ElastiCache | full | aws_elasticache_cluster |
| aws | EventBridge | full | aws_cloudwatch_event_rule |
| aws | Glue | full | aws_glue_catalog_database |
| aws | GuardDuty | full | aws_guardduty_detector |
| aws | IAM | full | aws_iam_role |
| aws | KMS | full | aws_kms_key |
| aws | Kinesis | full | aws_kinesis_stream |
| aws | Lambda | full | aws_lambda_function |
| aws | MSK | full | aws_msk_cluster |
| aws | OpenSearch | full | aws_opensearch_domain |
| aws | RDS | full | aws_db_instance, aws_rds_cluster |
| aws | Redshift | full | aws_redshift_cluster |
| aws | Route 53 | full | aws_route53_zone |
| aws | S3 | full | aws_s3_bucket |
| aws | SES | full | aws_ses_domain_identity |
| aws | SNS | full | aws_sns_topic |
| aws | SQS | full | aws_sqs_queue |
| aws | SageMaker | full | aws_sagemaker_endpoint |
| aws | Secrets Manager | full | aws_secretsmanager_secret |
| aws | Step Functions | full | aws_sfn_state_machine |
| aws | VPC | full | aws_vpc |
| aws | WAF | full | aws_wafv2_web_acl |
| aws | X-Ray | full | aws_xray_sampling_rule |
| aws | Lake Formation | full | aws_lakeformation_permissions |
| aws | QuickSight | full | aws_quicksight_dashboard |
| aws | MQ | full | aws_mq_broker |
| aws | IoT Core | full | aws_iot_thing |
| aws | App Mesh | full | aws_appmesh_mesh |
| aws | CodeDeploy | full | aws_codedeploy_app |
| aws | CloudFormation full import | full | aws_cloudformation_stack |
| aws | Shield | full | aws_shield_protection |
| aws | Security Hub | full | aws_securityhub_account |
| aws | Config | full | aws_config_config_rule |
| aws | Organizations | full | aws_organizations_organization |
| aws | Control Tower | full | aws_controltower_control |
| aws | Textract | full | aws_textract_adapter |
| aws | Transcribe | full | aws_transcribe_vocabulary |
| aws | Translate | full | aws_translate_text |
| aws | Rekognition | full | aws_rekognition_collection |
| aws | Comprehend | full | aws_comprehend_document_classifier |
| gcp | Apigee | full | google_apigee_organization |
| gcp | App Engine | full | google_app_engine_application |
| gcp | Artifact Registry | full | google_artifact_registry_repository |
| gcp | BigQuery | full | google_bigquery_dataset |
| gcp | Bigtable | full | google_bigtable_instance |
| gcp | Cloud Armor | full | google_compute_security_policy |
| gcp | Cloud Build | full | google_cloudbuild_trigger |
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
| gcp | Composer | full | google_composer_environment |
| gcp | Dataflow | full | google_dataflow_job |
| gcp | Dataproc | full | google_dataproc_cluster |
| gcp | Eventarc | full | google_eventarc_trigger |
| gcp | Filestore | full | google_filestore_instance |
| gcp | Firestore | guided | google_firestore_database |
| gcp | GKE | mapped | google_container_cluster |
| gcp | IAM | mapped | google_project_iam_member |
| gcp | Identity Platform | mapped | google_identity_platform_config |
| gcp | Logging | missing |  |
| gcp | Memorystore | mapped | google_redis_instance |
| gcp | Monitoring | missing |  |
| gcp | Persistent Disk | mapped | google_compute_disk |
| gcp | Pub/Sub | guided | google_pubsub_topic, google_pubsub_subscription |
| gcp | Secret Manager | mapped | google_secret_manager_secret |
| gcp | Spanner | guided | google_spanner_instance |
| gcp | Trace | missing |  |
| gcp | VPC | mapped | google_compute_network |
| gcp | Vertex AI | missing |  |
| gcp | Workflows | missing |  |
| gcp | Dataplex | missing | google_dataplex_lake, google_dataplex_zone |
| gcp | Looker | missing | google_looker_instance |
| gcp | Cloud Deploy | missing | google_clouddeploy_delivery_pipeline, google_clouddeploy_target |
| gcp | Error Reporting | missing |  |
| gcp | Profiler | missing |  |
| gcp | TPU | missing | google_tpu_node, google_tpu_v2_vm |
| gcp | Document AI | missing | google_document_ai_processor |
| gcp | Vision AI | missing |  |
| gcp | Speech-to-Text | missing | google_speech_custom_class, google_speech_phrase_set |
| gcp | Translation | missing |  |
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

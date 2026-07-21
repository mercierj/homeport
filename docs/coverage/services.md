| PROVIDER | SERVICE | STATUS | RESOURCES |
| --- | --- | --- | --- |
| aws | ALB | mapped | aws_lb |
| aws | ACM | mapped | aws_acm_certificate |
| aws | API Gateway | guided | aws_api_gateway_rest_api |
| aws | AppSync | guided | aws_appsync_graphql_api |
| aws | Athena | mapped | aws_athena_workgroup |
| aws | Bedrock | mapped | aws_bedrock_inference_profile |
| aws | CloudFront | mapped | aws_cloudfront_distribution |
| aws | CloudWatch | guided | aws_cloudwatch_metric_alarm, aws_cloudwatch_log_group, aws_cloudwatch_dashboard |
| aws | CodeBuild | guided | aws_codebuild_project |
| aws | CodePipeline | mapped | aws_codepipeline |
| aws | Cognito | mapped | aws_cognito_user_pool |
| aws | DynamoDB | mapped | aws_dynamodb_table |
| aws | EBS | mapped | aws_ebs_volume |
| aws | EC2 | mapped | aws_instance |
| aws | ECR | mapped | aws_ecr_repository |
| aws | ECS | mapped | aws_ecs_service, aws_ecs_task_definition |
| aws | EFS | mapped | aws_efs_file_system |
| aws | EKS | mapped | aws_eks_cluster |
| aws | EMR | mapped | aws_emr_cluster |
| aws | ElastiCache | mapped | aws_elasticache_cluster |
| aws | EventBridge | mapped | aws_cloudwatch_event_rule |
| aws | Glue | mapped | aws_glue_catalog_database |
| aws | GuardDuty | mapped | aws_guardduty_detector |
| aws | IAM | mapped | aws_iam_role |
| aws | KMS | mapped | aws_kms_key |
| aws | Kinesis | mapped | aws_kinesis_stream |
| aws | Lambda | mapped | aws_lambda_function |
| aws | MSK | mapped | aws_msk_cluster |
| aws | OpenSearch | mapped | aws_opensearch_domain |
| aws | RDS | mapped | aws_db_instance, aws_rds_cluster |
| aws | Redshift | mapped | aws_redshift_cluster |
| aws | Route 53 | mapped | aws_route53_zone |
| aws | S3 | guided | aws_s3_bucket |
| aws | SES | mapped | aws_ses_domain_identity |
| aws | SNS | guided | aws_sns_topic |
| aws | SQS | mapped | aws_sqs_queue |
| aws | SageMaker | mapped | aws_sagemaker_endpoint |
| aws | Secrets Manager | guided | aws_secretsmanager_secret |
| aws | Step Functions | guided | aws_sfn_state_machine |
| aws | VPC | mapped | aws_vpc |
| aws | WAF | mapped | aws_wafv2_web_acl |
| aws | X-Ray | mapped | aws_xray_sampling_rule |
| aws | Lake Formation | mapped | aws_lakeformation_permissions |
| aws | QuickSight | mapped | aws_quicksight_dashboard |
| aws | MQ | mapped | aws_mq_broker |
| aws | IoT Core | mapped | aws_iot_thing |
| aws | App Mesh | mapped | aws_appmesh_mesh |
| aws | CodeDeploy | mapped | aws_codedeploy_app |
| aws | CloudFormation full import | mapped | aws_cloudformation_stack |
| aws | Shield | mapped | aws_shield_protection |
| aws | Security Hub | mapped | aws_securityhub_account |
| aws | Config | mapped | aws_config_config_rule |
| aws | Organizations | mapped | aws_organizations_organization |
| aws | Control Tower | mapped | aws_controltower_control |
| aws | Textract | mapped | aws_textract_adapter |
| aws | Transcribe | mapped | aws_transcribe_vocabulary |
| aws | Translate | mapped | aws_translate_text |
| aws | Rekognition | mapped | aws_rekognition_collection |
| aws | Comprehend | mapped | aws_comprehend_document_classifier |
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
| gcp | Firestore | full | google_firestore_database |
| gcp | GKE | full | google_container_cluster |
| gcp | IAM | full | google_project_iam_member |
| gcp | Identity Platform | full | google_identity_platform_config |
| gcp | Logging | full | google_logging_project_sink |
| gcp | Memorystore | full | google_redis_instance |
| gcp | Monitoring | full | google_monitoring_alert_policy, google_monitoring_dashboard |
| gcp | Persistent Disk | full | google_compute_disk |
| gcp | Pub/Sub | full | google_pubsub_topic, google_pubsub_subscription |
| gcp | Secret Manager | full | google_secret_manager_secret |
| gcp | Spanner | full | google_spanner_instance |
| gcp | Trace | full | google_project_service |
| gcp | VPC | full | google_compute_network |
| gcp | Vertex AI | full | google_vertex_ai_endpoint |
| gcp | Workflows | full | google_workflows_workflow |
| gcp | Dataplex | full | google_dataplex_lake, google_dataplex_zone |
| gcp | Looker | full | google_looker_instance |
| gcp | Cloud Deploy | full | google_clouddeploy_delivery_pipeline, google_clouddeploy_target |
| gcp | Error Reporting | full | google_error_reporting_service |
| gcp | Profiler | full | google_profiler_service |
| gcp | TPU | full | google_tpu_node, google_tpu_v2_vm |
| gcp | Document AI | full | google_document_ai_processor |
| gcp | Vision AI | full | google_vision_ai_service |
| gcp | Speech-to-Text | full | google_speech_custom_class, google_speech_phrase_set |
| gcp | Translation | full | google_translation_service |
| azure | AI Search | full | azurerm_search_service |
| azure | AKS | full | azurerm_kubernetes_cluster |
| azure | API Management | full | azurerm_api_management |
| azure | App Gateway | full | azurerm_application_gateway |
| azure | App Insights | full | azurerm_application_insights |
| azure | App Service | full | azurerm_app_service |
| azure | Azure AD B2C | full | azurerm_aadb2c_directory |
| azure | Azure Cache | full | azurerm_redis_cache |
| azure | Azure CDN | full | azurerm_cdn_profile |
| azure | Azure DNS | full | azurerm_dns_zone |
| azure | Azure Firewall | full | azurerm_firewall |
| azure | Azure Functions | full | azurerm_function_app |
| azure | Azure Load Balancer | full | azurerm_lb |
| azure | Azure SQL | full | azurerm_mssql_database |
| azure | Azure Storage | full | azurerm_storage_container, azurerm_storage_account, azurerm_storage_share |
| azure | Azure VNet | full | azurerm_virtual_network |
| azure | Azure VM | full | azurerm_linux_virtual_machine, azurerm_windows_virtual_machine |
| azure | Container Apps | full | azurerm_container_app |
| azure | Container Instances | full | azurerm_container_group |
| azure | Container Registry | full | azurerm_container_registry |
| azure | Cosmos DB | full | azurerm_cosmosdb_account |
| azure | Data Factory | full | azurerm_data_factory |
| azure | Databricks | full | azurerm_databricks_workspace |
| azure | Event Grid | full | azurerm_eventgrid_topic |
| azure | Event Hubs | full | azurerm_eventhub |
| azure | Foundry/OpenAI | full | azurerm_cognitive_account |
| azure | Front Door | full | azurerm_frontdoor |
| azure | IoT Hub | full | azurerm_iothub |
| azure | Key Vault | full | azurerm_key_vault |
| azure | Log Analytics | full | azurerm_log_analytics_workspace |
| azure | Logic Apps | full | azurerm_logic_app_workflow |
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
| azure | Automation | missing | azurerm_automation_account, azurerm_automation_runbook |
| azure | Purview | missing | azurerm_purview_account |
| azure | Machine Learning | missing | azurerm_machine_learning_workspace |
| azure | Document Intelligence | missing | azurerm_cognitive_account |
| azure | Speech | missing | azurerm_cognitive_account |
| azure | Translator | missing | azurerm_cognitive_account |

# Nebu AWS Deployment Runbook

This runbook covers day-1 and day-2 operations for the Nebu AWS deployment
(ECS Fargate + ALB + RDS + Secrets Manager), provisioned via OpenTofu.

---

## Prerequisites

- AWS CLI configured with sufficient IAM permissions
- OpenTofu >= 1.6.0 installed (`tofu --version`)
- An ACM certificate ARN for your domain (must be in the same region as the ALB)
- An S3 bucket and DynamoDB table for the OpenTofu state backend (see `main.tf`)

> **Security prerequisite:** The S3 state bucket MUST have server-side encryption enabled
> (SSE-KMS or SSE-S3). OpenTofu state may contain sensitive infrastructure details.
> Enable encryption on the bucket before running `tofu init`:
> ```bash
> aws s3api put-bucket-encryption \
>   --bucket my-tofu-state \
>   --server-side-encryption-configuration \
>   '{"Rules":[{"ApplyServerSideEncryptionByDefault":{"SSEAlgorithm":"aws:kms"}}]}'
> ```

---

## Initial Deploy (3 commands)

1. **Initialize** — download providers and configure the backend:

   ```bash
   tofu init \
     -backend-config="bucket=my-tofu-state" \
     -backend-config="key=nebu/aws/terraform.tfstate" \
     -backend-config="region=eu-central-1" \
     -backend-config="dynamodb_table=tofu-locks"
   ```

2. **Plan** — review the changes that will be applied:

   ```bash
   tofu plan -var-file=terraform.tfvars
   ```

3. **Apply** — provision the infrastructure:

   ```bash
   tofu apply -var-file=terraform.tfvars
   ```

   After apply completes, note the `alb_dns_name` output — point your DNS CNAME or
   Route 53 ALIAS record to this value.

---

## Smoke Test

After deploy, verify the gateway is reachable via HTTPS:

```bash
curl https://<alb_dns_name>/_matrix/client/v3/versions
```

Expected response — a valid Matrix versions JSON, for example:

```json
{
  "versions": ["v1.1", "v1.2", "v1.3"],
  "unstable_features": {}
}
```

A `200 OK` with this body confirms the ALB, ECS gateway service, and health checks
are all operational.

---

## Rolling Update (redeploy a new container version)

Force ECS to pull and run the latest container image without infrastructure changes:

```bash
aws ecs update-service \
  --cluster nebu-<environment> \
  --service nebu-<environment>-gateway \
  --force-new-deployment \
  --region eu-central-1

aws ecs update-service \
  --cluster nebu-<environment> \
  --service nebu-<environment>-core \
  --force-new-deployment \
  --region eu-central-1
```

ECS will perform a rolling replacement with zero downtime (existing tasks stay up
until new tasks pass their health check).

---

## Secret Rotation

The following secrets are stored in AWS Secrets Manager under `nebu/<environment>/`:

| Secret path                          | Purpose                                      |
|--------------------------------------|----------------------------------------------|
| `nebu/<env>/db_password`             | RDS PostgreSQL master password               |
| `nebu/<env>/internal_secret`         | Gateway ↔ Core node registration PSK        |
| `nebu/<env>/oidc_client_secret`      | OIDC client secret for the identity provider |

### Rotate a secret

**Via AWS Console:**
1. Open Secrets Manager → locate `nebu/<environment>/<secret-name>`
2. Click **Retrieve secret value** to confirm the current value
3. Click **Edit** → enter the new value → Save

**Via AWS CLI:**
```bash
aws secretsmanager put-secret-value \
  --secret-id "nebu/<environment>/db_password" \
  --secret-string "NEW_STRONG_PASSWORD" \
  --region eu-central-1
```

After rotating `internal_secret` or `oidc_client_secret`, force a new ECS deployment
so tasks pick up the updated value (see **Rolling Update** above).

After rotating `db_password`, you must also update the RDS master password:
```bash
aws rds modify-db-instance \
  --db-instance-identifier nebu-<environment>-postgres \
  --master-user-password "NEW_STRONG_PASSWORD" \
  --apply-immediately \
  --region eu-central-1
```

---

## Teardown

Destroy all provisioned resources:

```bash
tofu destroy -var-file=terraform.tfvars
```

> **Warning:** The RDS instance has `deletion_protection = false` in dev by default.
> For production, set `deletion_protection = true` in `database.tf` and
> `skip_final_snapshot = false` in your `terraform.tfvars`.
> `tofu destroy` will fail on a deletion-protected instance — remove the protection
> first via the AWS Console or AWS CLI before destroying.

---

## Troubleshooting

| Symptom | First step |
|---|---|
| ALB health check failing | Check ECS task logs: `aws logs tail /ecs/nebu-<env>-gateway --follow` |
| ECS tasks not starting | Check Events in ECS console → look for `CannotPullContainerError` or secrets errors |
| 502 Bad Gateway | Verify security group `ecs_ingress_matrix_from_alb` allows port 8008 from the ALB SG |
| Secret not found on task start | Confirm IAM task execution role has `secretsmanager:GetSecretValue` on `nebu/<env>/*` |

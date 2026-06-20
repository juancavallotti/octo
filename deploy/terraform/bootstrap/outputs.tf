output "bucket_name" {
  description = "Name of the state bucket. Use it as the `bucket` in infra/ and release/ backend.tf."
  value       = google_storage_bucket.tfstate.name
}

# A check is imported by its ID (the UUID shown in the dashboard URL or returned
# by the API). Terraform then manages the existing check going forward.
terraform import stillbeat_check.nightly_backup 00000000-0000-0000-0000-000000000000

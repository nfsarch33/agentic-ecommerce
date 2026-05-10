variable "tenancy_ocid" {
  description = "OCI tenancy OCID. Set via OCI_TENANCY_OCID env var or terraform.tfvars."
  type        = string
}

variable "user_ocid" {
  description = "OCI user OCID for API authentication."
  type        = string
}

variable "fingerprint" {
  description = "Fingerprint of the OCI API signing key."
  type        = string
}

variable "private_key_path" {
  description = "Path to the OCI API signing private key PEM file."
  type        = string
}

variable "region" {
  description = "OCI region for resource deployment."
  type        = string
  default     = "ap-sydney-1"
}

variable "compartment_name" {
  description = "Name for the EC stack OCI compartment."
  type        = string
  default     = "ec-stack"
}

variable "vcn_cidr" {
  description = "CIDR block for the Virtual Cloud Network."
  type        = string
  default     = "10.0.0.0/16"
}

variable "public_subnet_cidr" {
  description = "CIDR block for the public subnet."
  type        = string
  default     = "10.0.1.0/24"
}

variable "private_subnet_cidr" {
  description = "CIDR block for the private subnet."
  type        = string
  default     = "10.0.2.0/24"
}

variable "mem0_shape" {
  description = "OCI compute shape for the mem0 instance."
  type        = string
  default     = "VM.Standard.A1.Flex"
}

variable "mem0_ocpus" {
  description = "Number of OCPUs for the mem0 Flex instance."
  type        = number
  default     = 2
}

variable "mem0_memory_gb" {
  description = "Memory in GB for the mem0 Flex instance."
  type        = number
  default     = 12
}

variable "mem0_image_ocid" {
  description = "OCID of the OS image for the mem0 compute instance. Use a canonical Ubuntu image for your region."
  type        = string
}

variable "ssh_public_key" {
  description = "SSH public key for compute instance access."
  type        = string
}

variable "qdrant_volume_size_gb" {
  description = "Size in GB for the Qdrant data block volume."
  type        = number
  default     = 50
}

variable "tags" {
  description = "Freeform tags applied to all resources."
  type        = map(string)
  default = {
    project     = "agentic-ecommerce"
    component   = "mem0"
    managed_by  = "terraform"
  }
}

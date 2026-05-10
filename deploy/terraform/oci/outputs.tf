output "compartment_id" {
  description = "OCID of the EC stack compartment."
  value       = oci_identity_compartment.ec_stack.id
}

output "vcn_id" {
  description = "OCID of the EC stack VCN."
  value       = oci_core_vcn.ec_vcn.id
}

output "mem0_instance_id" {
  description = "OCID of the mem0 compute instance."
  value       = oci_core_instance.mem0.id
}

output "mem0_public_ip" {
  description = "Public IP of the mem0 compute instance."
  value       = oci_core_instance.mem0.public_ip
}

output "mem0_private_ip" {
  description = "Private IP of the mem0 compute instance."
  value       = oci_core_instance.mem0.private_ip
}

output "public_subnet_id" {
  description = "OCID of the public subnet."
  value       = oci_core_subnet.ec_public.id
}

output "private_subnet_id" {
  description = "OCID of the private subnet."
  value       = oci_core_subnet.ec_private.id
}

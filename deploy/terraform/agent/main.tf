# Deploys an EC2 instance running the Helios agent.
#
# The instance is registered in the hub on first boot via user-data, then
# starts drift-checking whatever application is assigned to it.
variable "name" {
  default = "helios-agent"
}
variable "server_url" {
  description = "Central hub base URL, e.g. http://hub.internal:8080"
}
variable "token" {
  description = "Per-instance bootstrap token issued by the hub UI"
  sensitive   = true
}
variable "agent_url" {
  description = "URL where the built helios-agent binary can be downloaded"
}
variable "instance_type" {
  default = "t3.micro"
}
variable "ami" {
  default = "ami-0c55b159cbfafe1f0" # Amazon Linux 2023 (us-east-1); adjust per region
}
variable "subnet_id" {}
variable "vpc_security_group_ids" {
  type = list(string)
}
variable "key_name" {
  default = ""
}

resource "aws_instance" "agent" {
  ami                    = var.ami
  instance_type          = var.instance_type
  subnet_id              = var.subnet_id
  vpc_security_group_ids = var.vpc_security_group_ids
  key_name               = var.key_name == "" ? null : var.key_name

  iam_instance_profile = aws_iam_instance_profile.agent.name

  user_data_replace_on_change = true
  user_data                   = templatefile("${path.module}/agent-user-data.yaml.tftpl", {
    server     = var.server_url
    token      = var.token
    agent_url  = var.agent_url
  })

  tags = {
    Name = var.name
  }
}

resource "aws_iam_role" "agent" {
  name = "helios-agent-${var.name}"
  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect    = "Allow"
      Principal = { Service = "ec2.amazonaws.com" }
      Action    = "sts:AssumeRole"
    }]
  })
}

resource "aws_iam_instance_profile" "agent" {
  name = "helios-agent-${var.name}"
  role = aws_iam_role.agent.name
}

resource "aws_iam_role_policy" "agent" {
  name = "read-modify"
  role = aws_iam_role.agent.id
  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Effect   = "Allow"
        Action   = ["ec2:DescribeInstances", "ec2:DescribeTags"]
        Resource = "*"
      },
      {
        Effect   = "Allow"
        Action   = ["ssm:GetParameter"]
        Resource = "*"
      },
    ]
  })
}

output "instance_id" {
  value = aws_instance.agent.id
}
output "instance_ip" {
  value = aws_instance.agent.public_ip
}
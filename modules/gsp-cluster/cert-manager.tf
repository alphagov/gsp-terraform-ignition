data "aws_iam_policy_document" "trust_cert_manager" {
  statement {
    effect  = "Allow"
    actions = ["sts:AssumeRoleWithWebIdentity"]

    principals {
      type        = "Federated"
      identifiers = [module.k8s-cluster.oidc_provider_arn]
    }

    condition {
      test = "StringEquals"
      variable = "${replace(module.k8s-cluster.oidc_provider_url, "https://", "")}:sub"
      values = ["system:serviceaccount:gsp-system:gsp-cert-manager"]
    }
  }
}

data "aws_iam_policy_document" "cert_manager" {
  statement {
    effect = "Allow"

    actions = [
      "route53:GetHostedZone",
      "route53:ChangeResourceRecordSets",
      "route53:ListResourceRecordSets",
    ]

    resources = formatlist("arn:aws:route53:::hostedzone/%s", var.cluster_zone_ids)
  }

  statement {
    effect = "Allow"

    actions = [
      "route53:GetChange",
    ]

    resources = [
      "arn:aws:route53:::change/*",
    ]
  }

  statement {
    effect = "Allow"

    actions = [
      "route53:ListHostedZones",
      "route53:ListHostedZonesByName",
    ]

    resources = [
      "*",
    ]
  }
}

resource "aws_iam_policy" "cert_manager" {
  name        = "${var.cluster_name}_cert_manager"
  description = "Allow cert-manager to use the DNS01 challenge"

  policy = data.aws_iam_policy_document.cert_manager.json
}

resource "aws_iam_role" "cert_manager" {
  name = "${var.cluster_name}_cert_manager"

  assume_role_policy = data.aws_iam_policy_document.trust_cert_manager.json
}

resource "aws_iam_policy_attachment" "cert_manager" {
  name = "${var.cluster_name}_cert_manager"
  roles = [
    aws_iam_role.cert_manager.name,
  ]
  policy_arn = aws_iam_policy.cert_manager.arn
}


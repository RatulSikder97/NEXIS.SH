"""Deployment descriptors emitted for the fixture service.

The DevOps agent patches these dictionaries the way it would patch the YAML
they render to. Scenario targets:

  * `container_resources` — memory limit under the working set (OOMKilled loop).
  * `image_ref`           — floating `:latest` tag, so a pull can miss the
                            digest entirely (ImagePullBackOff).
  * `pod_disruption_budget` — minAvailable equal to the replica count, so no
                            pod may ever be evicted and a node drain hangs.
  * `canary_steps`        — jumps straight to 100% with no pause or analysis
                            step, so a bad revision takes all traffic.
  * `tls_cert`            — renewal window shorter than the issuance lead time.
"""

REPLICAS = 3


def container_resources():
    """CPU/memory request+limit for the API container."""
    return {
        "requests": {"cpu": "250m", "memory": "192Mi"},
        "limits": {"cpu": "500m", "memory": "256Mi"},
    }


def image_ref(registry="ghcr.io/nexis-sh", name="fixture-api"):
    """Fully-qualified image reference used by the deployment."""
    return f"{registry}/{name}:latest"


def pod_disruption_budget():
    """PDB guarding voluntary evictions."""
    return {"minAvailable": REPLICAS, "selector": {"app": "fixture-api"}}


def canary_steps():
    """Argo Rollouts canary plan."""
    return [{"setWeight": 100}]


def tls_cert():
    """Certificate metadata published to the renewal cron."""
    return {"common_name": "nexis.sh", "renew_before_days": 5, "lifetime_days": 90}


def rollback_target(history):
    """Pick the revision to roll back to after a failed deploy."""
    if not history:
        return None
    return history[-1]

// Phase 3.5 — Project settings (recovery policy editor) server shell.
//
// Loads the project + its current recovery policy. The policy endpoint may
// not exist yet (BE-A is in parallel); when it doesn't, we fall back to the
// policy block embedded on the Project row so the form still seeds with the
// last-known values.

import { cookies } from "next/headers";
import { notFound, redirect } from "next/navigation";

import { ProjectSettingsClient } from "./client";
import { projects, type RecoveryPolicy } from "@/lib/projects";

export default async function ProjectSettingsPage({
  params,
}: {
  params: Promise<{ id: string }>;
}) {
  const { id } = await params;

  const c = await cookies();
  const session = c.get("nexis_session");
  if (!session) redirect("/sign-in");
  const cookieHeader = `nexis_session=${session.value}`;

  const project = await projects.get(id, { cookie: cookieHeader });
  if (!project) notFound();

  // getPolicy may 404 today — fall back to the embedded recovery_policy on
  // the project row. The form still works; the PUT just creates a fresh row
  // when the backend finally lands.
  const policyFromEndpoint = await projects.getPolicy(id, {
    cookie: cookieHeader,
  });
  const policy: RecoveryPolicy = policyFromEndpoint ?? project.recovery_policy;

  return (
    <ProjectSettingsClient
      projectId={id}
      projectName={project.name}
      initialPolicy={policy}
      policyEndpointMissing={policyFromEndpoint === null}
    />
  );
}

import { z } from "zod";

import type {
  BranchOverview,
  CIRun,
  CIRunPage,
  CIWorkflow,
  CIWorkflowPage,
  DashboardData,
  Notification,
  OrganizationView,
  Repository,
  UserProfile,
} from "./api-types";

export const repositorySchema: z.ZodType<Repository> = z
  .object({
    id: z.string(),
    organizationId: z.string(),
    owner: z.string(),
    slug: z.string(),
    displayName: z.string(),
    description: z.string(),
    visibility: z.enum(["private", "internal", "public"]),
    loreRepositoryId: z.string(),
    loreUrl: z.string(),
    defaultBranch: z.string(),
    homepageUrl: z.string(),
    allowIssues: z.boolean(),
    allowMergeRequests: z.boolean(),
    topics: z.array(z.string()),
    issueCount: z.number(),
    mergeRequestCount: z.number(),
    starCount: z.number().optional(),
    watcherCount: z.number().optional(),
    viewerHasStarred: z.boolean().optional(),
    viewerIsWatching: z.boolean().optional(),
    archivedAt: z.string().nullable(),
    updatedAt: z.string(),
  })
  .loose();

export const organizationViewSchema: z.ZodType<OrganizationView> = z
  .object({
    id: z.string(),
    slug: z.string(),
    displayName: z.string(),
    description: z.string(),
    visibility: z.enum(["private", "internal", "public"]),
    createdAt: z.string(),
    websiteUrl: z.string(),
    contactEmail: z.string(),
    defaultRepositoryVisibility: z.enum(["private", "internal", "public"]),
    role: z.enum(["", "owner", "maintainer", "member"]),
    memberCount: z.number(),
    repositoryCount: z.number(),
    teamCount: z.number(),
  })
  .loose();

const notificationSchema: z.ZodType<Notification> = z
  .object({
    id: z.string(),
    topic: z.string(),
    title: z.string(),
    body: z.string(),
    href: z.string(),
    readAt: z.string().nullable(),
    createdAt: z.string(),
  })
  .loose();

export const dashboardSchema: z.ZodType<DashboardData> = z
  .object({
    repositories: z.array(repositorySchema),
    organizations: z.array(organizationViewSchema),
    notifications: z.array(notificationSchema),
    unreadNotifications: z.number(),
  })
  .loose();

export const repositoryListSchema = z.object({ repositories: z.array(repositorySchema) }).loose();

export const userProfileSchema: z.ZodType<UserProfile> = z
  .object({
    id: z.string(),
    username: z.string(),
    displayName: z.string(),
    email: z.string().nullable(),
    bio: z.string(),
    avatarUrl: z.string(),
    websiteUrl: z.string(),
    location: z.string(),
    company: z.string(),
    pronouns: z.string(),
    locale: z.string(),
    createdAt: z.string(),
    repositoryCount: z.number(),
  })
  .loose();

const branchSchema = z
  .object({
    id: z.string(),
    name: z.string(),
    category: z.string(),
    latestRevision: z.string(),
    creator: z.string(),
    createdAt: z.string(),
    current: z.boolean(),
    archived: z.boolean(),
  })
  .loose();

export const branchOverviewSchema: z.ZodType<BranchOverview> = z
  .object({
    branches: z.array(branchSchema),
    viewerCanPush: z.boolean(),
    viewerCanManageRules: z.boolean(),
  })
  .loose();

const ciRunSchema: z.ZodType<CIRun> = z
  .object({
    id: z.string(),
    workflowId: z.string(),
    workflowName: z.string(),
    workflowPath: z.string(),
    runNumber: z.number(),
    runAttempt: z.number(),
    rerunOf: z.string().optional(),
    eventName: z.string(),
    branch: z.string(),
    revision: z.string(),
    status: z.enum(["queued", "in_progress", "completed", "cancelled"]),
    conclusion: z.enum(["success", "failure", "cancelled", "skipped", "timed_out"]).nullable(),
    queuedAt: z.string(),
    startedAt: z.string().nullable(),
    completedAt: z.string().nullable(),
  })
  .loose();

export const ciRunPageSchema: z.ZodType<CIRunPage> = z
  .object({
    runs: z.array(ciRunSchema),
    totalCount: z.number(),
    page: z.number(),
    perPage: z.number(),
    hasMore: z.boolean(),
  })
  .loose();

const ciWorkflowSchema: z.ZodType<CIWorkflow> = z
  .object({
    id: z.string(),
    path: z.string(),
    name: z.string(),
    enabled: z.boolean(),
    state: z.enum(["active", "disabled", "error"]),
    errorCode: z.string().optional(),
    errorMessage: z.string().optional(),
    lastSeenRevision: z.string(),
    triggerConfig: z.custom<CIWorkflow["triggerConfig"]>(isRecord),
    updatedAt: z.string(),
  })
  .loose();

export const ciWorkflowPageSchema: z.ZodType<CIWorkflowPage> = z
  .object({
    workflows: z.array(ciWorkflowSchema),
    totalCount: z.number(),
    page: z.number(),
    perPage: z.number(),
    hasMore: z.boolean(),
    canWrite: z.boolean(),
  })
  .loose();

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

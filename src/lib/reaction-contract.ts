import { z } from "zod";

const reactionName = z.enum(["+1", "-1", "laugh", "confused", "heart", "hooray", "rocket", "eyes"]);

export const reactionSchema = z
  .object({
    reaction: reactionName,
    count: z.number().int().nonnegative(),
    viewerReacted: z.boolean(),
  })
  .strict();

export const optionalReactionsSchema = z.array(reactionSchema).optional();

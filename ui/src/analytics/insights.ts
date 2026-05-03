// Cross-tree analytics computed from raw Decision[] + Actor[].
//
// DATA MODEL (important — these three fields are INDEPENDENT):
//   - creator       : who created the decision (1 person)
//   - recommended_by: who made the recommendation, if any (0 or 1 person)
//   - decided_by    : who decided, when status === "decided" (1 or more)
// A decision can have any combination — creator may or may not also be the
// recommender; decider may or may not be either.
//
// "Accepted recommendation" = a DECIDED decision where:
//     is_recommended === true
//   OR
//     actual_choice && recommended_summary && actual_choice === recommended_summary

import type { Decision, Actor } from "@/api/types.gen";

export function isAccepted(d: Decision): boolean {
  if (d.status !== "decided") return false;
  if (d.is_recommended === true) return true;
  if (
    d.actual_choice &&
    d.recommended_summary &&
    d.actual_choice === d.recommended_summary
  ) {
    return true;
  }
  return false;
}

export function hasRecommendation(d: Decision): boolean {
  return Boolean(d.recommended_by) || Boolean(d.recommended_summary);
}

export function actorKind(
  actors: Actor[],
  handle: string | undefined,
): "human" | "agent" | "unknown" {
  if (!handle) return "unknown";
  const a = actors.find((x) => x.handle === handle);
  return a ? a.kind : "unknown";
}

export interface RateStat {
  total: number;
  accepted: number;
  rate: number | null;
}
export function withRate(s: { total: number; accepted: number }): RateStat {
  return { ...s, rate: s.total === 0 ? null : (s.accepted / s.total) * 100 };
}

// ---- Cross-tree summary -------------------------------------------------

export interface AgentHumanBreakdown {
  totalDecided: number;
  withRecommendation: number;
  delegationRate: number | null; // withRec / totalDecided
  agent: RateStat; // accepted / total agent recs
  human: RateStat; // accepted / total human recs
  unknown: RateStat;
}

export function computeAgentHumanBreakdown(
  decisions: Decision[],
  actors: Actor[],
): AgentHumanBreakdown {
  const decided = decisions.filter((d) => d.status === "decided");
  const withRec = decided.filter((d) => Boolean(d.recommended_by));
  const buckets = {
    agent: { total: 0, accepted: 0 },
    human: { total: 0, accepted: 0 },
    unknown: { total: 0, accepted: 0 },
  };
  for (const d of withRec) {
    const k = actorKind(actors, d.recommended_by);
    buckets[k].total += 1;
    if (isAccepted(d)) buckets[k].accepted += 1;
  }
  return {
    totalDecided: decided.length,
    withRecommendation: withRec.length,
    delegationRate:
      decided.length === 0 ? null : (withRec.length / decided.length) * 100,
    agent: withRate(buckets.agent),
    human: withRate(buckets.human),
    unknown: withRate(buckets.unknown),
  };
}

// ---- Per-user, three-facet model ---------------------------------------

export interface CreatorFacet {
  totalCreated: number;
  byStatus: Record<string, number>; // proposed/decided/out_of_scope/superseded

  // Subset: decisions you created where you were also the recommender
  alsoRecommender: number;
  // Of those that have been DECIDED (so we can score acceptance):
  alsoRecommenderDecided: RateStat; // accepted = recommendation followed
}

export interface RecommenderFacet {
  totalRecommended: number; // recommended_by === handle (any status)
  decidedCount: number; // ...where status === "decided"
  acceptance: RateStat; // accepted / decidedCount
  byKindOfDecider: {
    human: RateStat;
    agent: RateStat;
    unknown: RateStat;
  };
}

export interface DeciderFacet {
  totalDecided: number;
  followedRec: number;        // accepted recommendation (rec existed and matched)
  overrodeRec: number;        // rec existed but they chose differently
  noRecExisted: number;       // no recommendation existed
  acceptanceWhenRecExisted: RateStat;

  // Of the decisions where they followed a recommendation, who recommended?
  followedFromAgent: number;
  followedFromHuman: number;
  followedFromUnknown: number;

  // Trust profile: of all decided-with-rec, what % did they accept by recommender kind?
  agentTrust: RateStat; // total = decisions decided where recommender is agent
  humanTrust: RateStat;
}

export interface UserStats {
  handle: string;
  creator: CreatorFacet;
  recommender: RecommenderFacet;
  decider: DeciderFacet;
}

/**
 * Returns the actual Decision[] subset corresponding to a user's facet bucket.
 * Use to back the click-through modal so each stat is investigable.
 */
export type UserBucket =
  // creator facet
  | { facet: "creator"; key: "all" }
  | { facet: "creator"; key: "byStatus"; status: string }
  | { facet: "creator"; key: "alsoRecommender" }
  | { facet: "creator"; key: "alsoRecAccepted" }
  | { facet: "creator"; key: "alsoRecOverridden" }
  // recommender facet
  | { facet: "recommender"; key: "all" }
  | { facet: "recommender"; key: "decided" }
  | { facet: "recommender"; key: "accepted" }
  | { facet: "recommender"; key: "overridden" }
  | { facet: "recommender"; key: "byDeciderKind"; kind: "agent" | "human" | "unknown" }
  // decider facet
  | { facet: "decider"; key: "all" }
  | { facet: "decider"; key: "followedRec" }
  | { facet: "decider"; key: "overrodeRec" }
  | { facet: "decider"; key: "noRec" }
  | { facet: "decider"; key: "followedFromKind"; kind: "agent" | "human" | "unknown" }
  | { facet: "decider"; key: "trustKind"; kind: "agent" | "human" };

export function decisionsForUserBucket(
  handle: string,
  decisions: Decision[],
  actors: Actor[],
  bucket: UserBucket,
): Decision[] {
  if (bucket.facet === "creator") {
    const created = decisions.filter((d) => d.creator === handle);
    if (bucket.key === "all") return created;
    if (bucket.key === "byStatus")
      return created.filter((d) => d.status === bucket.status);
    const alsoRec = created.filter((d) => d.recommended_by === handle);
    if (bucket.key === "alsoRecommender") return alsoRec;
    const alsoRecDecided = alsoRec.filter((d) => d.status === "decided");
    if (bucket.key === "alsoRecAccepted") return alsoRecDecided.filter(isAccepted);
    if (bucket.key === "alsoRecOverridden")
      return alsoRecDecided.filter((d) => !isAccepted(d));
    return [];
  }

  if (bucket.facet === "recommender") {
    const recommended = decisions.filter((d) => d.recommended_by === handle);
    if (bucket.key === "all") return recommended;
    const recDecided = recommended.filter((d) => d.status === "decided");
    if (bucket.key === "decided") return recDecided;
    if (bucket.key === "accepted") return recDecided.filter(isAccepted);
    if (bucket.key === "overridden") return recDecided.filter((d) => !isAccepted(d));
    if (bucket.key === "byDeciderKind") {
      return recDecided.filter((d) => {
        const decider = (d.decided_by ?? [])[0];
        return actorKind(actors, decider) === bucket.kind;
      });
    }
    return [];
  }

  // decider facet
  const decidedByUser = decisions.filter(
    (d) => d.status === "decided" && (d.decided_by ?? []).includes(handle),
  );
  if (bucket.key === "all") return decidedByUser;
  if (bucket.key === "followedRec")
    return decidedByUser.filter((d) => hasRecommendation(d) && isAccepted(d));
  if (bucket.key === "overrodeRec")
    return decidedByUser.filter((d) => hasRecommendation(d) && !isAccepted(d));
  if (bucket.key === "noRec")
    return decidedByUser.filter((d) => !hasRecommendation(d));
  if (bucket.key === "followedFromKind") {
    return decidedByUser.filter(
      (d) =>
        hasRecommendation(d) &&
        isAccepted(d) &&
        actorKind(actors, d.recommended_by) === bucket.kind,
    );
  }
  if (bucket.key === "trustKind") {
    return decidedByUser.filter(
      (d) =>
        hasRecommendation(d) &&
        actorKind(actors, d.recommended_by) === bucket.kind,
    );
  }
  return [];
}

export function computeUserStats(
  handle: string,
  decisions: Decision[],
  actors: Actor[],
): UserStats {
  // ---- Creator facet
  const created = decisions.filter((d) => d.creator === handle);
  const byStatus = created.reduce(
    (acc, d) => {
      acc[d.status] = (acc[d.status] ?? 0) + 1;
      return acc;
    },
    {} as Record<string, number>,
  );
  const createdAlsoRec = created.filter((d) => d.recommended_by === handle);
  const createdAlsoRecDecided = createdAlsoRec.filter(
    (d) => d.status === "decided",
  );
  const creator: CreatorFacet = {
    totalCreated: created.length,
    byStatus,
    alsoRecommender: createdAlsoRec.length,
    alsoRecommenderDecided: withRate({
      total: createdAlsoRecDecided.length,
      accepted: createdAlsoRecDecided.filter(isAccepted).length,
    }),
  };

  // ---- Recommender facet
  const recommended = decisions.filter((d) => d.recommended_by === handle);
  const recDecided = recommended.filter((d) => d.status === "decided");

  const deciderKindBuckets = {
    human: { total: 0, accepted: 0 },
    agent: { total: 0, accepted: 0 },
    unknown: { total: 0, accepted: 0 },
  };
  for (const d of recDecided) {
    // Pick first decider as representative (decided_by is rarely multi)
    const decider = (d.decided_by ?? [])[0];
    const k = actorKind(actors, decider);
    deciderKindBuckets[k].total += 1;
    if (isAccepted(d)) deciderKindBuckets[k].accepted += 1;
  }

  const recommender: RecommenderFacet = {
    totalRecommended: recommended.length,
    decidedCount: recDecided.length,
    acceptance: withRate({
      total: recDecided.length,
      accepted: recDecided.filter(isAccepted).length,
    }),
    byKindOfDecider: {
      human: withRate(deciderKindBuckets.human),
      agent: withRate(deciderKindBuckets.agent),
      unknown: withRate(deciderKindBuckets.unknown),
    },
  };

  // ---- Decider facet
  const decidedByUser = decisions.filter(
    (d) => d.status === "decided" && (d.decided_by ?? []).includes(handle),
  );

  let followedRec = 0,
    overrodeRec = 0,
    noRecExisted = 0;
  let followedFromAgent = 0,
    followedFromHuman = 0,
    followedFromUnknown = 0;

  const trustBuckets = {
    agent: { total: 0, accepted: 0 },
    human: { total: 0, accepted: 0 },
    unknown: { total: 0, accepted: 0 },
  };

  for (const d of decidedByUser) {
    const recExisted = hasRecommendation(d);
    if (!recExisted) {
      noRecExisted += 1;
      continue;
    }
    const recKind = actorKind(actors, d.recommended_by);
    trustBuckets[recKind].total += 1;
    if (isAccepted(d)) {
      followedRec += 1;
      trustBuckets[recKind].accepted += 1;
      if (recKind === "agent") followedFromAgent += 1;
      else if (recKind === "human") followedFromHuman += 1;
      else followedFromUnknown += 1;
    } else {
      overrodeRec += 1;
    }
  }

  const decider: DeciderFacet = {
    totalDecided: decidedByUser.length,
    followedRec,
    overrodeRec,
    noRecExisted,
    acceptanceWhenRecExisted: withRate({
      total: followedRec + overrodeRec,
      accepted: followedRec,
    }),
    followedFromAgent,
    followedFromHuman,
    followedFromUnknown,
    agentTrust: withRate(trustBuckets.agent),
    humanTrust: withRate(trustBuckets.human),
  };

  return { handle, creator, recommender, decider };
}

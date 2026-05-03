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
  outstanding: number; // status === "proposed"
  resolved: number; // anything else
  byStatus: Record<string, number>;

  // Subset: created where they were also the recommender (any status).
  alsoRecommender: number;

  // Of RESOLVED creations: who provided the standing recommendation?
  // Counts decisions, not events. "none" = nobody recommended anything.
  resolvedByRecSource: {
    self: number;
    anotherAgent: number;
    anotherHuman: number;
    none: number;
  };
}

export interface RecommenderFacet {
  totalRecommended: number;
  decidedCount: number;
  acceptedCount: number;
  overriddenCount: number;
  acceptance: RateStat;

  // Who accepted vs who overrode this person's recommendations, split by
  // the actor kind making the call.
  acceptedBy: {
    self: number;
    anotherAgent: number;
    anotherHuman: number;
    unknown: number;
  };
  overriddenBy: {
    self: number;
    anotherAgent: number;
    anotherHuman: number;
    unknown: number;
  };
}

export interface DeciderFacet {
  totalDecided: number;
  followedRec: number;
  overrodeRec: number;
  noRecExisted: number;
  acceptanceWhenRecExisted: RateStat;

  // Of the decisions where they followed a recommendation, who recommended?
  followedFromSelf: number;
  followedFromOtherAgent: number;
  followedFromHuman: number;
  followedFromUnknown: number;

  // Per-source acceptance rate.
  bySource: {
    self: RateStat;
    anotherAgent: RateStat;
    anotherHuman: RateStat;
  };

  // Headline AUTONOMY metric: of decisions where some agent (self OR other)
  // recommended something AND this person decided, what fraction did they
  // follow? High = comfortable delegating to AI; low = manual override-prone.
  agenticAutonomy: RateStat;
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
  | {
      facet: "recommender";
      key: "byDeciderBucket";
      bucket: "self" | "otherAgent" | "otherHuman" | "unknownActor";
    }
  // decider facet
  | { facet: "decider"; key: "all" }
  | { facet: "decider"; key: "followedRec" }
  | { facet: "decider"; key: "overrodeRec" }
  | { facet: "decider"; key: "noRec" }
  | {
      facet: "decider";
      key: "followedFromSource";
      source: "self" | "otherAgent" | "human" | "unknown";
    }
  | {
      facet: "decider";
      key: "trustSource";
      source: "self" | "otherAgent" | "human";
    };

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
    if (bucket.key === "overridden")
      return recDecided.filter((d) => !isAccepted(d));
    if (bucket.key === "byDeciderBucket") {
      return recDecided.filter((d) => {
        const decider = (d.decided_by ?? [])[0];
        if (bucket.bucket === "self") return decider === handle;
        if (decider === handle) return false;
        const k = actorKind(actors, decider);
        if (bucket.bucket === "otherAgent") return k === "agent";
        if (bucket.bucket === "otherHuman") return k === "human";
        if (bucket.bucket === "unknownActor") return k === "unknown";
        return false;
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

  const matchSource = (d: Decision) => {
    const recBy = d.recommended_by;
    if (bucket.key === "followedFromSource" || bucket.key === "trustSource") {
      const src = bucket.source;
      if (src === "self") return recBy === handle;
      if (recBy === handle) return false;
      const k = actorKind(actors, recBy);
      if (src === "otherAgent") return k === "agent";
      if (src === "human") return k === "human";
      if (src === "unknown") return k === "unknown";
    }
    return false;
  };

  if (bucket.key === "followedFromSource") {
    return decidedByUser.filter(
      (d) => hasRecommendation(d) && isAccepted(d) && matchSource(d),
    );
  }
  if (bucket.key === "trustSource") {
    return decidedByUser.filter((d) => hasRecommendation(d) && matchSource(d));
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
  const outstanding = created.filter((d) => d.status === "proposed").length;
  const resolvedList = created.filter((d) => d.status !== "proposed");

  const resolvedByRecSource = {
    self: 0,
    anotherAgent: 0,
    anotherHuman: 0,
    none: 0,
  };
  for (const d of resolvedList) {
    if (!d.recommended_by) {
      resolvedByRecSource.none += 1;
    } else if (d.recommended_by === handle) {
      resolvedByRecSource.self += 1;
    } else {
      const k = actorKind(actors, d.recommended_by);
      if (k === "agent") resolvedByRecSource.anotherAgent += 1;
      else if (k === "human") resolvedByRecSource.anotherHuman += 1;
      else resolvedByRecSource.none += 1;
    }
  }

  const creator: CreatorFacet = {
    totalCreated: created.length,
    outstanding,
    resolved: resolvedList.length,
    byStatus,
    alsoRecommender: createdAlsoRec.length,
    resolvedByRecSource,
  };

  // ---- Recommender facet
  const recommended = decisions.filter((d) => d.recommended_by === handle);
  const recDecided = recommended.filter((d) => d.status === "decided");

  type Bucket = "self" | "anotherAgent" | "anotherHuman" | "unknown";
  const acceptedBy = { self: 0, anotherAgent: 0, anotherHuman: 0, unknown: 0 };
  const overriddenBy = { self: 0, anotherAgent: 0, anotherHuman: 0, unknown: 0 };
  let acceptedCount = 0;
  let overriddenCount = 0;
  const bucketFor = (deciderHandle: string | undefined): Bucket => {
    if (deciderHandle === handle) return "self";
    const k = actorKind(actors, deciderHandle);
    if (k === "agent") return "anotherAgent";
    if (k === "human") return "anotherHuman";
    return "unknown";
  };
  for (const d of recDecided) {
    const decider = (d.decided_by ?? [])[0];
    const b = bucketFor(decider);
    if (isAccepted(d)) {
      acceptedBy[b] += 1;
      acceptedCount += 1;
    } else {
      overriddenBy[b] += 1;
      overriddenCount += 1;
    }
  }

  const recommender: RecommenderFacet = {
    totalRecommended: recommended.length,
    decidedCount: recDecided.length,
    acceptedCount,
    overriddenCount,
    acceptance: withRate({ total: recDecided.length, accepted: acceptedCount }),
    acceptedBy,
    overriddenBy,
  };

  // ---- Decider facet
  const decidedByUser = decisions.filter(
    (d) => d.status === "decided" && (d.decided_by ?? []).includes(handle),
  );

  let followedRec = 0,
    overrodeRec = 0,
    noRecExisted = 0;
  let followedFromSelf = 0,
    followedFromOtherAgent = 0,
    followedFromHuman = 0,
    followedFromUnknown = 0;

  const trustBuckets = {
    self: { total: 0, accepted: 0 },
    otherAgent: { total: 0, accepted: 0 },
    human: { total: 0, accepted: 0 },
  };

  for (const d of decidedByUser) {
    if (!hasRecommendation(d)) {
      noRecExisted += 1;
      continue;
    }
    const recBy = d.recommended_by;
    let bucket: "self" | "otherAgent" | "human" | null;
    if (recBy === handle) bucket = "self";
    else {
      const k = actorKind(actors, recBy);
      bucket = k === "agent" ? "otherAgent" : k === "human" ? "human" : null;
    }
    if (bucket) {
      trustBuckets[bucket].total += 1;
      if (isAccepted(d)) trustBuckets[bucket].accepted += 1;
    }

    if (isAccepted(d)) {
      followedRec += 1;
      if (bucket === "self") followedFromSelf += 1;
      else if (bucket === "otherAgent") followedFromOtherAgent += 1;
      else if (bucket === "human") followedFromHuman += 1;
      else followedFromUnknown += 1;
    } else {
      overrodeRec += 1;
    }
  }

  const agenticTotal = trustBuckets.self.total + trustBuckets.otherAgent.total;
  // "self" only counts as agentic if THIS actor IS an agent.
  const userIsAgent = actorKind(actors, handle) === "agent";
  const agenticAccepted =
    (userIsAgent ? trustBuckets.self.accepted : 0) +
    trustBuckets.otherAgent.accepted;
  const agenticDenominator =
    (userIsAgent ? trustBuckets.self.total : 0) + trustBuckets.otherAgent.total;

  const decider: DeciderFacet = {
    totalDecided: decidedByUser.length,
    followedRec,
    overrodeRec,
    noRecExisted,
    acceptanceWhenRecExisted: withRate({
      total: followedRec + overrodeRec,
      accepted: followedRec,
    }),
    followedFromSelf,
    followedFromOtherAgent,
    followedFromHuman,
    followedFromUnknown,
    bySource: {
      self: withRate(trustBuckets.self),
      anotherAgent: withRate(trustBuckets.otherAgent),
      anotherHuman: withRate(trustBuckets.human),
    },
    agenticAutonomy: withRate({
      total: agenticDenominator,
      accepted: agenticAccepted,
    }),
  };
  // Suppress lint: agenticTotal is a debug aid; remove if unused.
  void agenticTotal;

  return { handle, creator, recommender, decider };
}

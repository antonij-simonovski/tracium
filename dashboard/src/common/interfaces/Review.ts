/** missing: the run or span gave no usable output. */
export type Verdict = 'correct' | 'incorrect' | 'missing';

/** One JSON field of a reviewed output; empty output/corrected means the field is absent there. */
export interface ReviewField {
  path: string;
  output: string;
  corrected: string;
  correct: boolean;
  /** The output left out a field the correction has. */
  missing: boolean;
}

export interface RunReview {
  trace_id: string;
  /** Empty for a review of the whole run. */
  span_id: string;
  span_name: string;
  workspace_id: string;
  workflow_name: string;
  workflow_version: string;
  verdict: Verdict;
  output: string;
  corrected_output: string | null;
  note: string;
  reviewed_by: string;
  /** 'api' when an ingest key sent the review. */
  source: 'user' | 'api';
  created_at: string;
  updated_at: string;
  fields: ReviewField[];
}

export interface ReviewInput {
  verdict?: Verdict;
  /** Grades this output instead of the traced one. */
  output?: string;
  corrected_output?: string;
  /** Dotted field paths to set on the output; the rest grade as correct. */
  corrections?: Record<string, unknown>;
  note?: string;
}

export interface VersionAccuracy {
  /** Span name; '' is the whole run. */
  span: string;
  version: string;
  reviewed_runs: number;
  correct_runs: number;
  accuracy: number;
}

export interface ReviewedSpan {
  name: string;
  reviewed: number;
}

export interface FieldAccuracy {
  path: string;
  reviewed: number;
  wrong: number;
  /** Of the wrong ones, how many the output left out. */
  missing: number;
  accuracy: number;
}

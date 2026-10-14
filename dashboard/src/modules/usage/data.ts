import type { UsageData } from './interfaces';
import type { UserId } from '../../common/ids';

export const USAGE_DATA: UsageData = {
  range: {
    label: "Last 30 days",
    start: "Apr 1, 2026",
    end:   "Apr 30, 2026",
    days:  30,
  },
  totalCost:      4.3812,
  totalCostPrev:  3.8204,
  totalRuns:      5751,
  totalRunsPrev:  4988,
  dailySeries: Array.from({ length: 30 }, (_, i) => {
    const d = i + 1;
    const cost = 0.12 + Math.sin(i / 4) * 0.04;
    const runs = Math.round(140 + Math.sin(i / 3) * 30);
    return {
      label: `Apr ${d}`,
      cost: Math.max(0.04, cost),
      runs,
    };
  }),
  tokenSeries: Array.from({ length: 30 }, (_, i) => {
    const runs = Math.round(140 + Math.sin(i / 3) * 30);
    return {
      label: `Apr ${i + 1}`,
      input: Math.round(runs * (4385 + Math.sin(i / 5) * 400)),
      output: Math.round(runs * (1190 + Math.cos(i / 4) * 160)),
      runs,
    };
  }),
  users: [
    { id: "u_umbrella"     as UserId, name: "Umbrella Production", cost: 2.8412, costPrev: 2.4880, runs: 3204, runsPrev: 2961, avg: 0.000887, trend: [80,85,92,88,96,102,108,112]},
    { id: "u_umbrella_dev" as UserId, name: "Umbrella Dev",         cost: 0.9812, costPrev: 0.9078, runs: 1802, runsPrev: 1741, avg: 0.000545, trend: [30,32,35,33,38,40,42,44]},
    { id: "u_umbrella_stg" as UserId, name: "Umbrella Staging",     cost: 0.3204, costPrev: 0.3283, runs: 612,  runsPrev: 641,  avg: 0.000524, trend: [18,20,19,22,21,20,22,21]},
    { id: "u_personal"  as UserId, name: "Personal",           cost: 0.0384, costPrev: 0.0291, runs: 133,  runsPrev: 96,   avg: 0.000289, trend: [2,3,4,3,5,6,5,8]},
  ],
  workflows: [
    { name: "summarize-comments",             cost: 0.4234, costPrev: 0.3767, runs: 847,  runsPrev: 812,  avg: 0.000500, model: "claude-3-5-haiku-20241022" },
    { name: "generate-clock-out-description", cost: 0.2891, costPrev: 0.2733, runs: 412,  runsPrev: 430,  avg: 0.000702, model: "claude-sonnet-4-5"         },
    { name: "classify-intent",                cost: 0.1204, costPrev: 0.0986, runs: 1203, runsPrev: 1040, avg: 0.000100, model: "claude-3-5-haiku-20241022" },
    { name: "rewrite-message",                cost: 0.3104, costPrev: 0.3804, runs: 156,  runsPrev: 184,  avg: 0.001990, model: "claude-sonnet-4-5"         },
    { name: "extract-entities",               cost: 0.0892, costPrev: 0.0864, runs: 328,  runsPrev: 308,  avg: 0.000272, model: "claude-3-5-haiku-20241022" },
    { name: "detect-sentiment",               cost: 0.0412, costPrev: 0.0378, runs: 612,  runsPrev: 580,  avg: 0.000067, model: "claude-3-5-haiku-20241022" },
    { name: "summarize-thread",               cost: 0.0834, costPrev: 0.0823, runs: 89,   runsPrev: 92,   avg: 0.000937, model: "claude-sonnet-4-5"         },
    { name: "moderate-content",               cost: 0.0892, costPrev: 0.0751, runs: 2104, runsPrev: 1880, avg: 0.000042, model: "claude-3-5-haiku-20241022" },
  ],
  models: [
    { name: "claude-3-5-haiku-20241022", cost: 2.4812, runs: 4291, inputTokens: 12_482_100, outputTokens: 3_214_002, color: "#6366f1" },
    { name: "claude-sonnet-4-5",         cost: 1.6204, runs: 1212, inputTokens:  4_820_400, outputTokens: 1_480_200, color: "#8b5cf6" },
    { name: "claude-opus-4-5",           cost: 0.2796, runs: 248,  inputTokens:  1_117_810, outputTokens:   305_798, color: "#a78bfa" },
  ],
};

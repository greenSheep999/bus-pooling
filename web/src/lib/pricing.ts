import { MICRO } from "./utils";

/** 下单前的费用预览
 *
 *  ⚠️ **这是 mock 阶段的脚手架，不是计费实现。**
 *
 *  真实计费**只在服务端**：`POST /api/me/pull/estimate` 返回各项金额，
 *  前端直接渲染返回值。后端就绪后**删掉本文件**，改用 `useEstimate()`。
 *
 *  为什么不留在前端算：
 *  - 计费规则是内部信息，不该随打包产物发到浏览器
 *  - 前端一份、后端一份 = 必然漂移，用户看到的预估跟实际扣款不一致
 *
 *  在此之前所有预览都走这一个函数（原来两个组件各写一遍，会各自漂移）。
 */
export function previewFees(opts: {
  /** 服务端给的最终单价 */
  unitPrice: number;
  count: number;
  /** 是否已应用优惠码 */
  couponApplied?: boolean;
}): {
  unitPrice: number;
  keyCost: number;
  singlePullFee: number;
  serviceFee: number;
  total: number;
} {
  const unitPrice = opts.couponApplied
    ? Math.round(opts.unitPrice * (1 - COUPON_PREVIEW))
    : opts.unitPrice;

  const keyCost = unitPrice * opts.count;
  const singlePullFee = opts.count === 1 ? Math.round(keyCost * SINGLE_PULL_PREVIEW) : 0;
  const serviceFee = Math.max(0, Math.round(opts.count)) * MICRO;

  return {
    unitPrice,
    keyCost,
    singlePullFee,
    serviceFee,
    total: keyCost + singlePullFee + serviceFee,
  };
}

/* 预览用的近似值 · 实际以服务端返回为准 */
const COUPON_PREVIEW = 0.05;
const SINGLE_PULL_PREVIEW = 0.2;

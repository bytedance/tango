// Copyright 2026 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// An approximation of the exponential function optimized for runtime use.

/*
```python
#!/usr/bin/env python3
# -*- coding: utf-8 -*-

import numpy as np
import matplotlib.pyplot as plt
import struct

# =========================
# exp approx with (numpy + math) dependencies
# =========================
def exp_approx_vec(x):
    LN2 = 0.6931471805599453
    MIN_EXP = 1e-14
    x = np.array(x, dtype=np.float64)
    k = np.floor(-x / LN2).astype(int)
    r = x + k * LN2  # r ∈ [0, ln2]
    y = 1 + r * (1 + r * (0.5 + r * (1.0 / 6.0 + r * (1.0 / 24.0 + r * (1.0 / 120.0)))))
    y = y * np.power(0.5, k)
    y = np.maximum(y, MIN_EXP)
    return y


# =========================
# exp approx without math dependencies
# =========================
LN2 = 0.6931471805599453
INV_LN2 = 1.4426950408889634  # 1 / ln(2)
MIN_EXP = 1e-14


def float_from_bits(u):
    return struct.unpack(">d", struct.pack(">Q", u))[0]


def fast_floor(x):
    i = int(x)
    if float(i) > x:
        return i - 1
    return i


def pow2_neg(k):
    exp = 1023 - k
    if exp <= 0:
        return 0.0
    if exp >= 0x7FF:
        return 1e308
    return float_from_bits(exp << 52)


def exp_approx_nomath_vec(x):
    x = np.array(x, dtype=np.float64)
    y = np.empty_like(x)

    for i, xi in enumerate(x):
        k = fast_floor(-xi * INV_LN2)
        r = xi + k * LN2
        yi = 1.0 + r * (
            1.0 + r * (0.5 + r * (1.0 / 6.0 + r * (1.0 / 24.0 + r * (1.0 / 120.0))))
        )
        yi *= pow2_neg(k)
        if yi < MIN_EXP:
            yi = MIN_EXP
        y[i] = yi
    return y

# =========================
# standard sigmoid
# =========================
def sigmoid(x):
    x = np.array(x, dtype=np.float64)
    return 1.0 / (1.0 + np.exp(-x))


# =========================
# based on exp_approx sigmoid
# =========================
def sigmoid_approx(x):
    return 1.0 / (1.0 + exp_approx_vec(-x))


def sigmoid_approx_nomath(x):
    return 1.0 / (1.0 + exp_approx_nomath_vec(-x))


# =========================
# simulation main
# =========================
def main():
    x = np.linspace(-100, 100, 2000)

    sig_exact = sigmoid(x)
    sig_approx = sigmoid_approx(x)
    sig_nomath = sigmoid_approx_nomath(x)

    # =========================
    # output plots
    # =========================
    plt.figure(figsize=(8, 5))
    plt.plot(x, sig_exact, label="sigmoid (exact)", linewidth=2)
    plt.plot(x, sig_approx, label="sigmoid (exp_approx)", linestyle="--")
    plt.plot(x, sig_nomath, label="sigmoid (exp_approx_nomath)", linestyle=":")
    plt.xlabel("x")
    plt.ylabel("sigmoid(x)")
    plt.title("Sigmoid vs Exp-Approx Sigmoid (x ∈ [-100, 100])")
    plt.legend()
    plt.grid(True)
    plt.tight_layout()
    plt.show()

    # =========================
    # absolute error
    # =========================
    abs_error_nomath = sig_nomath - sig_exact
    plt.figure(figsize=(8, 5))
    plt.plot(x, abs_error_nomath, label="absolute error (nomath)")
    plt.axhline(0, linestyle="--", linewidth=1)
    plt.xlabel("x")
    plt.ylabel("error")
    plt.title("Absolute Error: sigmoid(exp_approx_nomath) - sigmoid")
    plt.legend()
    plt.grid(True)
    plt.tight_layout()
    plt.show()

    # =========================
    # relative error
    # =========================
    eps = 1e-18
    rel_error_nomath = abs_error_nomath / (sig_exact + eps)
    plt.figure(figsize=(8, 5))
    plt.plot(x, rel_error_nomath, label="relative error (nomath)")
    plt.xlabel("x")
    plt.ylabel("relative error")
    plt.title("Relative Error: sigmoid(exp_approx_nomath)")
    plt.legend()
    plt.grid(True)
    plt.tight_layout()
    plt.show()

if __name__ == "__main__":
    main()
*/

package runtime

const (
	ln2    = 0.6931471805599453
	invLn2 = 1.4426950408889634 // 1 / ln(2)
	minExp = 1e-14
)

// fastFloor replace math.Floor
func fastFloor(x float64) int {
	i := int(x)
	if float64(i) > x {
		return i - 1
	}
	return i
}

// pow2Neg returns 2^-k
func pow2Neg(k int) float64 {
	exp := 1023 - k
	if exp <= 0 {
		return 0.0
	}
	if exp >= 0x7FF {
		return 1e308
	}
	return float64frombits(uint64(exp) << 52)
}

// expApprox computes an approximate exponential function.
func expApprox(x float64) float64 {
	k := fastFloor(-x * invLn2)
	r := x + float64(k)*ln2
	y := 1.0 + r*(1.0+r*(0.5+r*(1.0/6.0+r*(1.0/24.0+r*(1.0/120.0)))))
	y *= pow2Neg(k)
	// clamp
	if y < minExp {
		y = minExp
	}
	return y
}

// sigmoidApprox computes an approximate sigmoid function using expApprox.
// the cost of sigmoidApprox(x)'s call is about 40ns ~ 80ns.
func sigmoidApprox(x float64, k float64, target float64) float64 {
	return 1.0 / (1.0 + expApprox(-k*(x-target)))
}

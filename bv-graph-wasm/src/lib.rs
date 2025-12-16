//! High-performance graph algorithms for bv static viewer.
//!
//! This crate provides WASM-compiled graph algorithms that run in the browser,
//! enabling fast dependency analysis without server roundtrips.

use wasm_bindgen::prelude::*;

mod graph;
mod algorithms;
mod advanced;
mod whatif;
mod subgraph;
mod reachability;

pub use graph::DiGraph;

/// Initialize panic hook for better error messages in browser console.
#[wasm_bindgen(start)]
pub fn init() {
    #[cfg(feature = "console_error_panic_hook")]
    console_error_panic_hook::set_once();
}

/// Get the crate version.
#[wasm_bindgen]
pub fn version() -> String {
    env!("CARGO_PKG_VERSION").to_string()
}

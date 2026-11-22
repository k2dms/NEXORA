// The popup island's entry: the custom element is registered by the component itself
// (svelte:options customElement), the document is NEXORA-owned (nexora-popup.html), and the snapshot
// crosses as a property set by the shell (doc 22 §13 invariant 4 — the shell owns the state, the popup
// is a view). Invariant 2: this entry starts nothing — no command fires on mount.
import '../ui/Popup.svelte';

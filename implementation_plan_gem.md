# Smart Autocomplete Engine

This document outlines the research and implementation plan for building a highly optimized, "smart" autocomplete engine in Go using only the standard library, as requested. The system will learn from past inputs, provide prefix completions, guess next words, and tolerate typos. 

## 1. Algorithm Research & Selection

To build the "smartest available" autocomplete from scratch without external dependencies, we need a multi-layered algorithmic approach. 

### A. Prefix Matching: Weighted Radix Tree (Compressed Trie)
*   **How it works**: A variation of a Trie where nodes with a single child are merged. It drastically saves memory compared to a standard Trie. 
*   **Why we need it**: When the user types "S", we traverse down the "S" node to find all words starting with "S".
*   **Smartness Factor (Frequency Tracking)**: Every time a user finalizes a search (e.g., hits enter or selects an option), we increment the "weight" (frequency count) of that exact word in the Tree. When a user queries a prefix, the engine traverses the subtree, collects all children, and sorts them by their pre-recorded weights.

### B. Typo Tolerance: Levenshtein Distance + Trie Search
*   **How it works**: Calculates the minimum number of single-character edits (insertions, deletions, substitutions) required to change one word into another.
*   **Why we need it**: Users make typos. If they type "aple", the Trie might yield 0 results. We then scan the Trie for words within a Levenshtein distance of 1 or 2 from "aple" and return the highest-weighted results (e.g., "apple").

### C. Contextual Next-Word Prediction: N-Gram Markov Model
*   **How it works**: A probability model that tracks which words follow other words. For example, `P(word3 | word1, word2)`. In Go, this can be represented as `map[string]map[string]int` (where the key is the current context, and the value is a map of next possible words and their frequencies).
*   **Why we need it**: When someone types "how to ", we don't just want to prefix-match "to". We want to suggest "how to build", "how to code", based on what other people historically searched for after typing "how to".

## 2. Go Project Structure

We will adhere to standard Go layout practices:

```text
/home/mbow/code/search/
├── cmd/
│   └── api/
│       └── main.go              # Application entry point, server startup
├── internal/
│   ├── engine/
│   │   ├── trie.go              # Radix/Trie implementation with frequency weighting
│   │   ├── fuzzy.go             # Levenshtein distance calculations
│   │   ├── ngram.go             # Markov chain logic for next-word prediction
│   │   └── engine.go            # Orchestrator combining Trie + Fuzzy + Ngram
│   └── server/
│       └── handlers.go          # HTTP API handlers (Search, Learn)
├── web/
│   ├── index.html               # The frontend UI
│   ├── styles.css               # Premium CSS styling
│   └── app.js                   # JavaScript to handle keystrokes and display results
└── go.mod                       # Go module file
```

## 3. Communication: Debounced REST API vs. WebSockets

To make the frontend feel magical and "super dynamic":
1.  **The API**: We will build two main endpoints:
    *   `GET /api/suggest?q=...` : Returns up to `N` smartest suggestions.
    *   `POST /api/learn` : When the user actually selects a suggestion or submits a search, we send it here to increment the data structures.
2.  **The Frontend Tech**: We'll use a standard, polished HTML/CSS/JS frontend. We'll implement a **debounce mechanism** (~100-200ms) in vanilla JavaScript so we aren't flooding our Go backend with requests if the user is typing 120 WPM, but we remain highly responsive.

## User Review Required

> [!IMPORTANT]
> Since we are building this as a learning exercise, using only the Go Standard Library means we will be writing the data structures from scratch. 
> 
> Does the overarching algorithm proposed (Weighted Prefix Trie + N-Gram Context + Typo Tolerance) align with your expectations of the "smartest possible"? 
> 
> **Do you want to start with just the Weighted Prefix Trie, or would you like to build the full 3-layer system at once?**

## Verification Plan
1.  **Automated Tests**: I will write robust Go unit tests for `trie.go` and `ngram.go` to prove that "hello" outranks "helicopter" if it has been searched more often.
2.  **Manual Verification**: We will boot up a local Go server at `http://localhost:8080`, view the UI, and manually train the engine by submitting a few queries, verifying that subsequent prefixes appropriately predict our trained queries.

# hn-grep

Match stories by keywords or domain in Hacker News' Top 100. See [here].

## Why?

I wanted a way to filter the top 100 Hacker News posts by topics I care about. Also, I
thought it’d be neat to get notified if one of my writeups hits the front page. This is a
tiny site that filters the top posts and just shows the ones that matter to me.

## How it works

Here's how it all comes together:

- A small Go [CLI] runs every morning at 8 am (CET) using [GitHub Actions].
- It fetches the top posts from Hacker News the HN API.
- Relevant posts are filtered by keywords and domains I've set up.
- The filtered results are used to build an `index.html` page.
- This triggers a Cloudflare Pages build, which deploys the updated results.

[here](https://hn-grep.rednafi.com)
[cli](./main.go)
[github actions](.github/workflows/ci.yml)

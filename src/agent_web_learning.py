# agent_web_learning.py
"""
This module provides a simple real-world integration for agents to fetch and summarize web content about their jobs.
"""
import requests
from bs4 import BeautifulSoup


def fetch_and_summarize_job_info(job_title, max_chars=500):
    """
    Fetches the first Wikipedia page for the job and returns a summary.
    """
    search_url = f"https://en.wikipedia.org/wiki/{job_title.replace(' ', '_')}"
    try:
        resp = requests.get(search_url, timeout=10)
        if resp.status_code != 200:
            return f"No Wikipedia page found for {job_title}."
        soup = BeautifulSoup(resp.text, 'html.parser')
        paragraphs = soup.find_all('p')
        summary = ''
        for p in paragraphs:
            text = p.get_text().strip()
            if text:
                summary += text + ' '
            if len(summary) > max_chars:
                break
        return summary[:max_chars] + ('...' if len(summary) > max_chars else '')
    except Exception as e:
        return f"Error fetching info: {e}"

# Example usage:
if __name__ == "__main__":
    job = "Software engineer"
    print(fetch_and_summarize_job_info(job))

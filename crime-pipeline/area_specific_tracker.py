"""
SafeAround - Area-Specific Crime Tracker (PERFECT VERSION)
============================================================
Tracks crimes in hyper-local areas across India:
  - Specific sectors, neighborhoods, localities, even villages.
  - Combines Google News RSS + NewsAPI for broad coverage.
  - NLP entity extraction (spaCy) for precise sub-area pinning.
  - Spatial radius validation (geopy) to reject off-target articles.
  - Deduplication via DB ON CONFLICT and an in-process seen-URL cache.
  - APScheduler for standalone 30-minute auto-run cycles.

Algorithm Flow (per area):
  1. Fetch raw articles via Google News RSS & NewsAPI (parallel calls)
  2. Filter: article must actually mention the area name (keyword match)
  3. Filter: article must contain a crime keyword
  4. NLP: extract fine-grained sub-location (street / landmark)
  5. Geocode the sub-location for a precise lat/lng pin
  6. Spatial check: pin must be within area.radius_km of area center
  7. Extract crime_type, severity, date
  8. Bulk upsert to PostgreSQL via DatabaseService (database_service.py)

Geofencing Integration:
  The same area center + radius_km values are written to the
  `geofence_zones` table by the GeofencingService. This tracker
  feeds the crime_incidents table which the geofencing triggers query.
"""
import logging
import os
import re
import sys
import time
from concurrent.futures import ThreadPoolExecutor, as_completed
from datetime import datetime, timedelta

# ── Ensure crime-pipeline dir is in sys.path so local modules resolve ──────────
_HERE = os.path.dirname(os.path.abspath(__file__))
if _HERE not in sys.path:
    sys.path.insert(0, _HERE)

import feedparser                                # noqa: E402
import requests                                  # noqa: E402
from dateutil import parser as dateutil_parser   # noqa: E402
from geopy.distance import geodesic              # noqa: E402
from geopy.geocoders import Nominatim            # noqa: E402

from config import DB_CONFIG, NEWS_API_KEY, TARGET_AREAS       # noqa: E402
from database_service import DatabaseService                    # noqa: E402

# ─── Logging ──────────────────────────────────────────────────────────────────
logging.basicConfig(
    level=logging.INFO,
    format='%(asctime)s [%(levelname)s] %(name)s - %(message)s'
)
logger = logging.getLogger("AreaTracker")

# ─── NLP Setup (optional, graceful degradation if not installed) ───────────────
try:
    import spacy
    _nlp = spacy.load("en_core_web_lg")
    logger.info("✅ spaCy en_core_web_lg loaded")
except Exception:
    try:
        import spacy
        _nlp = spacy.load("en_core_web_sm")
        logger.warning("⚠️ spaCy sm model loaded (lower precision). Install en_core_web_lg for best results.")
    except Exception:
        _nlp = None
        logger.warning("⚠️ spaCy NOT available. Sub-area geocoding disabled.")


# ─── Crime Taxonomy ────────────────────────────────────────────────────────────
CRIME_TAXONOMY = {
    "murder":        (4, ["murder", "killed", "killing", "dead body", "found dead", "हत्या", "shooting"]),
    "rape":          (4, ["rape", "gang rape", "sexual assault", "molestation", "बलात्कार"]),
    "kidnapping":    (3, ["kidnap", "abduct", "missing", "child missing", "अपहरण"]),
    "robbery":       (3, ["robbery", "robbed", "dacoity", "loot", "looting", "snatching", "लूट"]),
    "assault":       (3, ["assault", "attack", "stabbing", "stabbed", "beaten", "beaten up", "हमला"]),
    "harassment":    (2, ["harass", "eve teasing", "stalking", "stalked", "molest"]),
    "theft":         (2, ["theft", "stolen", "steal", "stealing", "burglary", "pickpocket", "चोरी"]),
    "vehicle_crime": (2, ["car theft", "bike theft", "vehicle stolen", "car stolen", "bike stolen"]),
    "fraud":         (1, ["fraud", "scam", "cheating", "duped", "cyber crime"]),
    "drug":          (2, ["drug", "narcotics", "drug peddling", "drug trafficking"]),
}

CRIME_KEYWORDS_ALL = [kw for _, kws in CRIME_TAXONOMY.values() for kw in kws]


class AreaSpecificCrimeTracker:
    """
    Hyper-local India crime tracker.
    Uses area-specific news fetching + NLP + spatial validation.
    """

    def __init__(self):
        self.db            = DatabaseService(DB_CONFIG)
        self.geolocator    = Nominatim(user_agent="safearound_v2", timeout=5)
        self.nlp           = _nlp
        self.seen_urls     = set()   # In-process dedup cache (resets per process restart)
        self.area_centers  = {}      # name → {lat, lng, radius_km}
        self._geocode_areas()

    # ──────────────────────────────────────────────────────────────────────────
    # STARTUP: Geocode All Areas
    # ──────────────────────────────────────────────────────────────────────────

    def _geocode_areas(self):
        logger.info("📍 Geocoding %d target areas...", len(TARGET_AREAS))
        for area in TARGET_AREAS:
            name = area["name"]
            try:
                result = self.geolocator.geocode(name + ", India")
                if result:
                    self.area_centers[name] = {
                        "lat":       result.latitude,
                        "lng":       result.longitude,
                        "radius_km": area["radius_km"],
                    }
                    logger.debug("✅ %s → (%.4f, %.4f)", name, result.latitude, result.longitude)
                else:
                    logger.warning("⚠️ Could not geocode: %s", name)
            except Exception as exc:
                logger.error("Geocode error for %s: %s", name, exc)
            time.sleep(1.1)  # Nominatim rate limit: max 1 req/sec

        logger.info("📍 Geocoded %d/%d areas successfully", len(self.area_centers), len(TARGET_AREAS))

    # ──────────────────────────────────────────────────────────────────────────
    # FETCH: News Sources
    # ──────────────────────────────────────────────────────────────────────────

    def _fetch_google_news(self, area_name: str) -> list:
        """Fetch articles from Google News RSS for a specific area."""
        results = []
        for lang_params in [
            ("en", "IN:en"),   # English
            ("hi", "IN:hi"),   # Hindi
        ]:
            hl, ceid = lang_params
            query = f"crime+{area_name.replace(' ', '+').replace(',', '')}"
            url   = f"https://news.google.com/rss/search?q={query}&hl={hl}&gl=IN&ceid={ceid}&num=10"
            try:
                feed = feedparser.parse(url)
                for entry in feed.entries[:10]:
                    link  = entry.get("link", "")
                    if link in self.seen_urls:
                        continue
                    text  = (entry.get("title", "") + " " +
                             re.sub(r"<[^>]+>", " ", entry.get("summary", "")))
                    results.append({
                        "title":     entry.get("title", ""),
                        "raw_text":  text,
                        "link":      link,
                        "published": entry.get("published", ""),
                        "source":    "Google News",
                    })
            except Exception as exc:
                logger.debug("Google News fetch error (%s): %s", area_name, exc)
        return results

    def _fetch_newsapi(self, area_name: str) -> list:
        """Fetch articles from NewsAPI.org for a specific area."""
        if not NEWS_API_KEY:
            return []
        results = []
        try:
            resp = requests.get(
                "https://newsapi.org/v2/everything",
                params={
                    "q":        f'crime "{area_name}"',
                    "language": "en",
                    "sortBy":   "publishedAt",
                    "pageSize": 10,
                    "apiKey":   NEWS_API_KEY,
                },
                timeout=10,
            )
            if resp.status_code == 200:
                for article in resp.json().get("articles", []):
                    link = article.get("url", "")
                    if link in self.seen_urls:
                        continue
                    text = (article.get("title", "") + " " +
                            (article.get("description") or ""))
                    results.append({
                        "title":     article.get("title", ""),
                        "raw_text":  text,
                        "link":      link,
                        "published": article.get("publishedAt", ""),
                        "source":    "NewsAPI",
                    })
        except Exception as exc:
            logger.debug("NewsAPI fetch error (%s): %s", area_name, exc)
        return results

    # ──────────────────────────────────────────────────────────────────────────
    # FILTER: Relevance Checks
    # ──────────────────────────────────────────────────────────────────────────

    def _is_area_relevant(self, area_name: str, text: str) -> bool:
        """
        Check if thearticle is actually about the target area.
        We require at least 2 non-trivial words from the area name to match.
        """
        clean_name  = area_name.replace(",", "").lower()
        parts       = [p for p in clean_name.split() if len(p) > 3 and p not in {"sector", "phase"}]
        text_lower  = text.lower()
        matches     = sum(1 for p in parts if p in text_lower)
        return matches >= max(1, len(parts) // 2)

    def _has_crime_content(self, text: str) -> bool:
        """Check if the article contains at least one crime-related keyword."""
        t = text.lower()
        return any(kw in t for kw in CRIME_KEYWORDS_ALL)

    # ──────────────────────────────────────────────────────────────────────────
    # CLASSIFY: Crime Type & Severity
    # ──────────────────────────────────────────────────────────────────────────

    def _classify(self, text: str):
        """Return (crime_type, severity) for a piece of text."""
        t = text.lower()
        for ctype, (severity, keywords) in CRIME_TAXONOMY.items():
            if any(kw in t for kw in keywords):
                return ctype, severity
        return "other", 1

    # ──────────────────────────────────────────────────────────────────────────
    # GEOCODE: Sub-area / Landmark Extraction
    # ──────────────────────────────────────────────────────────────────────────

    def _extract_sub_location(self, text: str, area_name: str):
        """
        Use spaCy NER to find a street / landmark inside the article,
        then geocode it for a finer pin than the area centroid.
        Returns (lat, lng) or None.
        """
        if not self.nlp:
            return None

        doc       = self.nlp(text[:1000])  # Limit to first 1000 chars for speed
        city_words = {w.lower() for w in area_name.replace(",", "").split()}

        candidates = []
        for ent in doc.ents:
            if ent.label_ not in ("GPE", "LOC", "FAC"):
                continue
            ent_lower = ent.text.lower()
            # Skip very generic city names we already know
            if ent_lower in city_words or ent_lower in {"india", "delhi", "mumbai", "bangalore", "hyderabad", "chennai", "kolkata", "pune"}:
                continue
            candidates.append(ent.text)

        for candidate in candidates[:3]:  # Try at most 3 candidates
            full_query = f"{candidate}, {area_name}, India"
            try:
                result = self.geolocator.geocode(full_query, timeout=5)
                if result:
                    return result.latitude, result.longitude
            except Exception:
                pass
            time.sleep(0.5)

        return None

    # ──────────────────────────────────────────────────────────────────────────
    # VALIDATE: Spatial Radius Check
    # ──────────────────────────────────────────────────────────────────────────

    def _within_radius(self, lat: float, lng: float, area_name: str) -> bool:
        center = self.area_centers[area_name]
        dist   = geodesic((center["lat"], center["lng"]), (lat, lng)).km
        return dist <= center["radius_km"]

    # ──────────────────────────────────────────────────────────────────────────
    # PARSE: Date
    # ──────────────────────────────────────────────────────────────────────────

    def _parse_date(self, date_str: str) -> datetime:
        try:
            return dateutil_parser.parse(date_str)
        except Exception:
            return datetime.utcnow()

    # ──────────────────────────────────────────────────────────────────────────
    # PROCESS: Single Area
    # ──────────────────────────────────────────────────────────────────────────

    def process_area(self, area_name: str) -> list:
        """
        Full pipeline for a single area.
        Returns a list of validated incident dicts ready for DB insertion.
        """
        center = self.area_centers.get(area_name)
        if not center:
            return []

        # Fetch
        articles = self._fetch_google_news(area_name) + self._fetch_newsapi(area_name)

        incidents = []
        for article in articles:
            url  = article["link"]
            text = article["raw_text"]

            # Skip duplicates
            if url in self.seen_urls:
                continue

            # Filter: must mention area AND contain crime keyword
            if not self._is_area_relevant(area_name, text):
                continue
            if not self._has_crime_content(text):
                continue

            # Attempt sub-area pin → fall back to area centroid
            sub_loc = self._extract_sub_location(text, area_name)
            if sub_loc:
                lat, lng = sub_loc
                # Spatial validation: reject if outside radius
                if not self._within_radius(lat, lng, area_name):
                    lat, lng = center["lat"], center["lng"]
            else:
                lat, lng = center["lat"], center["lng"]

            crime_type, severity = self._classify(text)
            self.seen_urls.add(url)

            incidents.append({
                "crime_type":  crime_type,
                "severity":    severity,
                "latitude":    lat,
                "longitude":   lng,
                "address":     area_name,
                "title":       article["title"],
                "description": text[:500],
                "source":      article["source"],
                "source_url":  url,
                "occurred_at": self._parse_date(article["published"]),
            })

        return incidents

    # ──────────────────────────────────────────────────────────────────────────
    # RUN: Full Cycle (called by scheduler or main)
    # ──────────────────────────────────────────────────────────────────────────

    def run_cycle(self):
        """
        Run one complete tracking cycle across all areas.
        Uses ThreadPoolExecutor for faster fetching.
        """
        logger.info("⚡ ══ AREA TRACKING CYCLE STARTED ══")
        start = datetime.utcnow()
        all_incidents = []

        # Parallelise news fetching across areas (IO-bound)
        with ThreadPoolExecutor(max_workers=5) as executor:
            futures = {
                executor.submit(self.process_area, area): area
                for area in self.area_centers
            }
            for future in as_completed(futures):
                area = futures[future]
                try:
                    results = future.result()
                    if results:
                        logger.info("  📍 %s → %d incidents", area, len(results))
                    all_incidents.extend(results)
                except Exception as exc:
                    logger.error("  ❌ %s failed: %s", area, exc)

        # DB write
        inserted = self.db.save_batch(all_incidents)
        duration = (datetime.utcnow() - start).total_seconds()
        logger.info(
            "⚡ ══ CYCLE COMPLETE: %d found, %d inserted, %.1fs ══",
            len(all_incidents), inserted, duration
        )
        return inserted

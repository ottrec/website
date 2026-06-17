<!--
    Copyright (c) 2026 Patrick Gaskin.
    This content is not under LICENSE, and may not be used anywhere other than ottrec.ca.
-->

# How I ensure the quality of ottrec.ca data

*June 17, 2026*

I put a lot of work into ensuring that you can rely on the data from ottrec.ca. I designed the process carefully (no LLMs involved), and tested it extensively over multiple years of facility pages. I also manually verified many samples of the data.

This is article is aimed at technical users or other developers.

## Scraping

This is the most delicate part since the facility pages are human-written and frequently change format in subtle ways. It is especially important that I don't accidentally discard possible schedule data, and that I parse things deterministicallly and precisely (rather than use a LLM like some similar projects) to ensure that the data is accurate.

I do this in three stages: crawling, extraction, and parsing.

### Crawling

I start by refreshing the list of facilities from the main facilities page on the Ottawa website. This is more reliable than hardcoding the list since it changes every now and then, and it's also better than looking at the other pages on the Ottawa website since the others are manually updated.

I then crawl all facility pages, also keeping track of the modified date from the page itself. I store this separately so I can use it to test the scraper on all historical versions of the pages to avoid introducing regressions when making changes.

### Extraction

This stage is purely concerned about extracting as much relevant information from the page as possible, and splitting it into a basic set of fields. I do as little parsing in this stage as possible so I can later differentiate parse failures from information which simply isn't there.

In each facility page, I look for the schedule sections by finding the drop-downs containing schedule tables. All schedule pages have been layed out this way, and it's an intrinsic part of the template they use, so this should catch any possible schedules.

I also extract the facility information and the facility-wide schedule changes, notices, and special hours. I do not attempt to parse those notices since they're hand-written and change very frequently. Instead, I include them as-is and display them when necessary (discussed more later).

For the schedule groups, I extract the special hours by matching the block title. I do the same for the reservation links and possible "reservation required" blurb. Then, I iterate over the entire schedule table (while also preserving the title and splitting it into the facility/activity/date range if possible), and exhaustively collect all times and dates, even if it is malformed (this is important since they frequently make typos, and I don't want to silently drop possible times).

### Parsing

This part is the most complicated since I'm parsing hand-written data which frequently has formatting changes and sometimes has typos. I always preserve the original un-parsed fields in the data as a fallback; the parsed values are marked and stored separately.

For the facility address, I use a geocoder to convert it into the facility longitude/latitude. Every time a new address is seen, I also review the results manually, possibly adding overrides if it isn't resolved perfectly (this frequently happens with rural facilities or weird postal codes).

For the activity names, I do some simple find/replace normalization on it to make it usable for filtering (e.g, replacing something like "skating" with "skate"). I then make a best-effort attempt to split the base activity name into a field by itself purely for nicer display. I also cut out known segments like the "reservation required" text and store them as a flag to use later.

For the schedule titles, I first attempt to cut out the facility name if included, handling various edge-cases like accents and whitespace. I then attempt to find something date-like at the end, with very loose matching of various forms of day/month names so I can get alerts if the actual date-parsing fails. I then parse this date (and any non-date words around it like "after", "to", or "until") to make it usable for filtering. If any part of the date parsing fails, I emit an warning, and do not attempt to filter the schedule by date when displaying it (to ensure that data isn't accidentally missed).

One complication of the schedule date parsing is that the dates aren't always fully-qualified (they frequently don't include years). To resolve this cleanly, I do NOT store fully-qualified dates at parse time; I keep all fields (weekday/day/month/year) separately. When I actually use them for the website, I have a series of heuristics based on the other schedule dates and the current schedule time, plus additional verification that, for example, the weekday makes sense given the day/month/year (this usually catches any typos from the city).

For the schedule table, I first iterate over all headers (being careful to handle the edge-case where they don't always line up perfectly) and parse them into weekdays (or dates for fixed schedules) (emitting a prominent error if this fails to avoid producing misleading data). I then split all time slots into times, warning about any unexpected non-time text in them (to catch typos and formatting changes). I then parse the times, and validate that they make sense (e.g., valid hour/minute, no extremely long durations, start is before end, etc), being careful to handle edge cases like french time, 24h time, am/pm only on one side, and so on. If any time cannot be parsed unambiguously, I emit an warning (which gets displayed on the website) instead of including the possible incorrect time.

I am almost always able to successfully unambiguously parse all times and dates across all pages, and nearly every case which failed between April 2025-2026 was due to a typo from the city.

After I initially wrote the parser, I wrote some tools to compare it to some of the other projects which attempt to do the same thing, and ended up finding and reporting bugs in many of them (most of them were about missing times, some incorrectly parsed weirdly formatted dates/times).

I also review all errors and warnings from the parser, which frequently (multiple times per month) catch minor typos in the schedules, which I then report to the city.

## Dataset

I publish two datasets: one with just the raw scraped fields as originally written, and another simplified one with the times flattened and heuristics applied.

The raw dataset has a stable structure which matches the inherent form of the data, and will include the information as written. The parsed form of certain fields is also included if the parsing was unambiguous and successfule, with a list of possible errors included for anything which couldn't be parsed. This data is never changed once saved.

The simplified dataset includes additional heuristics (e.g., fully qualified schedule date ranges, whether reservation is/is not/might be required for a given activity, etc). These are updated over time as I find additional ways to normalize or validate the data, and I have many checks to ensure the result makes sense.

Importantly, I do NOT attempt to figure out which schedule is relevant for a specific day, nor do I attempt to parse the cancellation text, since this is free-form and inherently ambiguous given the way the city website is structured. In any page where I show schedules, I always include the cancellation text and schedule dates as originally written.

## Display

Displaying the data in a non-misleading way is also extremely important. Each page layout requires considering different cases.

### All-in-one schedule page

This one is straightforward. I simply render the data in the same layout as the official schedule pages, but use the normalized names/times where the parsing was unambiguous to improve the readability of the data.

I do have one additional thing: I mark ones with reservations required specially. This requires resolving the requirement for each activity, which isn't always unambiguous (some facilities say reservation required for the entire group, then have some activities say they don't require reservation; some are the other way around; some mark each activity; some just include reservation links to imply that the activities require reservation; and others do a combination). I ended up heuristically determining two boolean flags: whether an activity requires reservation, and whether the requirement is definite or not.

### Advanced search

This one is a little bit more complex, but still mostly straightforward.

I need to ensure that a search for facilities and activities match even if dashes, accents, or periods (e.g., "St. Laurent", "J.A. Dulude") don't match perfectly. I perform unicode normalization on the names used for filtering and the search term, and match the result fuzzily.

I also need to ensure that time/date ranges which weren't able to be parsed don't cause in incomplete results. For this, I simply include all data which couldn't be filtered unambiguously since a few false matches (obvious to the human reading them) are better than missing ones (which would make the data unable to be relied on).

### Activity facilities/availability

For this, I need to be able to categorize the activity names into broader categories like swimming, skating, and pickleball. I also need to be extremely careful not to accidentally exclude different variations of the name introduced later.

Instead of matching on exact activity names, I look for the most minimal substring needed to differentiate it from other activities, erring on the side of matching too much (also because it's obvious to the human reading it). For example, for hockey, I search "hockey", "puck", and "ringette". For aquafit, I simply look for "aqua".

### Map

For this, I need to ensure the filters never filter out too much, even if I add new ones later or part of the data changes.

The easier filters are the weekday and time range. Unparsed times always match regardless of the filter.

There are also category filters (based on the logic I described above) and activity filters. If a category is selected, the activity filters become excludes so newly added activities for a category automatically get included unless explicitly excluded by the user.

### Today/upcoming activity list

This is the most readable view, especially on small screens. However, it also requires the most simplification. It is of the utmost importance that I never exclude a time which the user may be looking for, that I never include a non-current time (e.g., from a future schedule, or a regular one where there's a holiday one), and that I include all caveats (schedule changes, holiday schedules, reservations, etc) prominently to avoid misleading users.

This one required the most thinking to implement since, as described earlier, I do not attempt to parse cancellations or determine whether a given day uses a holiday schedule.

The solution ended up being very simple. I only allow filtering on fields which parse unambiguously (facility/activity name, activity time, etc), and always include any ones where I was unable to parse it. Then, I include a series of warnings wherever they may affect the validity of the time:

- If there's any holiday schedule for facility (i.e., ones with specific dates in the headers close to the current date).
- If there's any overlapping schedules for the same date range (either in the same schedule group, or with the overlapping activity names).
- If there's any schedule changes for the schedule group.
- If there's any facility-wide notices, special hours, or schedule changes.
- If reservation may be required.

Usually this means around 15% of the time/activity entries will include some kind of warning. To make this easier to use, I made it so that clicking on it would show the relevant information, and I also added a link to the official city page so the user can easily decide for themselves.

## Conclusion

I hope this article helps explain some of the design decisions I made when creating this website, and also helps increase your confidence in the quality of the data.

Feel free to send me an email if you have any questions.

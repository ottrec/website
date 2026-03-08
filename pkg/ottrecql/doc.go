// Package ottrecql implements a query language for filtering schedules.
/*
Expressions consist of match function calls combined with logical operators.

These expressions are used to filter the activity times, removing empty
activities/schedules/groups/facilities aftewards.

Whitespace is insignificant and may be used for readability.

The schedule timezone, i.e., America/Toronto, is implied for dates and times.

# Types

Strings are double-quoted with with backslash escape sequences.

    "example"
    "a quote (\") and a backslash (\\)"

Dates consist of a calendar date in YYYY-MM-DD format, or the special literal
"today".

    2024-03-15
    today

Times are specified in one of the following formats, or the special literal
"now".

    HH:MM             - 24-hour time, e.g., 14:30
    HH:MMa or HH:MMam - 12-hour AM time, e.g., 9:00a or 9:00am
    HH:MMp or HH:MMpm - 24-hour PM time, e.g., 3:30p or 3:30pm
    now               - the current clock time (no date is implied)

Time ranges are specified as two dash-separated times. If the end time precedes
the start time, it is assumed to go into the next day (but will still only match
activities starting on the specified weekday).

	1:00a-6:00p       - 1 AM to 6 PM
	01:00-6:00p       - 1 AM to 6 PM
	00:00-02:00       - midnight to 2 AM the next day

Weekdays are specified as a two-letter, three-letter, or full name
(case-insensitive).

    Mo  Mon  Monday
    Tu  Tue  Tuesday
    We  Wed  Wednesday
    Th  Thu  Thursday
    Fr  Fri  Friday
    Sa  Sat  Saturday
    Su  Sun  Sunday

Numbers are a 32-bit floating-point value. NaN and Inf are not valid.

    3.14
    100
    -0.5

# Operators

Boolean operators may be written as keywords or symbols. Parentheses may be used
to group expressions. From highest to lowest precedence:

    (...)     - group
    not, !    - negation
    and, &&   - conjunction
    or, ||    - disjunction

# Functions

Function names are lowercase alphanumeric strings. Arguments are usually
comma-separated.

The schdate() function matches schedule groups which apply on the specified
date. If a schedule group does not have a valid date range, it will not be
filtered out as a result of the expression.

    schdate(date)

    schdate(today)      - match all schedules groups which are currently applicable
    schdate(2025-12-24) - match all schedules groups which apply to Dec 24, 2025

The time() function matches activity weekdays/dates/times. If an activity time
was not parsed successfully or has an invalid weekday, it will not be filtered
out as a result of the expression. The @ can be left out if only specifying
times or weekdays/dates. Exact dates will match activities scheduled for that
weekday or with the specific date.

    time([weekday...|date...] @ [time...|timerange...])

    time(now)                           - activities at the current time on any weekday
    time(today)                         - activities on the current weekday at any time
    time(today @ now)                   - activities on the current weekday at the current time
                                          (you probably also want to include `&& schdate(today)`
                                          to only show ones for the current schedule)
    time(mo tu we th fr)                - activities on weekdays
    time(sa su @ 12:00)                 - activities with a time overlapping noon on weekends
    time(sa su @ 18:00-01:00)           - activities starting on the weekend from 6pm to 1am
    time(mo @ 6:00a-10:00a 6:00p-9:00p) - activities from 6-10am or 6-9pm on Monday


The facility() function matches facility names against any of the specified
strings. Matches are done fuzzily, where each case-insensitive word of the query
must correspond to a prefix of a word in the facility name in the same order.
Punctuation is normalized.

    facility("splash")                   - matches "Splash Wave Pool"
    facility("st laurent")               - matches "St. Laurent Complex"
    facility("St. Laurent")              - matches "St. Laurent Complex"
    facility("hunt club riverside park") - matches "Hunt Club-Riverside Park Community Centre"
    facility("rj kennedy")               - matches "R.J. Kennedy Community Centre and Arena"
    facility("tom brown", "jim durrell") - matches "Tom Brown Arena" and "Jim Durrell Recreation Centre"
    facility("francois dupuis rec")      - matches "François Dupuis Recreation Centre"

The activity() function is like facility(), but matches activity names (both
normalized and as specified in the original schedule).

Generally, activity names are matched against the infinitive form (e.g, "skate"
rather than "skating"), but future versions may perform additional
normalization.

    activity("lane swim", "public swim")
    activity("figure skate")
    activity("badminton")
    activity("hockey")
    activity("pub skat")

The latlng() function matches facilities within a certain radius (kilometers) of
the specified coordinates.

    latlng(45.42620, -75.69205, 2) - facilities within 2km of rideau station

# Compatibility

This query language will not be changed in backwards-incompatible ways.

For best results, ensure facility/activity queries are as minimal as possible to
match the desired activities to ensure stuff isn't missed in the future. For
example, you may want to search for "splash" instead of "Splash Wave Pool",
"lane sw" instead of "lane swim", or "skat" (plus a negated "figure") instead of
"public skate" + "adult skate" + "family skate".

# Performance

Query parsing and evaluation is done linearly without backtracking, and with as
few memory allocations as possible.

Queries are optimized for repeated evaluations. Operations are short-circuited
where possible.

To limit the time and memory cost of user-specified queries, you should set a
limit on the maximum length before parsing, and the [Cost] before compiling.
*/
package ottrecql

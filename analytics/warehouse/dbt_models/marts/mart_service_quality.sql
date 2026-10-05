-- ============================================================================
-- Mart Model: Service Quality Analytics
-- Purpose: Monitor service health, SLAs, and customer satisfaction
-- ============================================================================

{{
    config(
        materialized='table',
        tags=['mart', 'quality', 'sla']
    )
}}

with ride_quality as (
    select
        date(requested_at) as date,
        count(*) as total_rides,
        count(*) filter (where status = 'completed') as completed_rides,
        count(*) filter (where status = 'cancelled') as cancelled_rides,
        
        -- Matching performance
        avg(matching_time_seconds) as avg_matching_time_seconds,
        percentile_cont(0.95) within group (order by matching_time_seconds) as p95_matching_time_seconds,
        percentile_cont(0.99) within group (order by matching_time_seconds) as p99_matching_time_seconds,
        
        -- ETA accuracy
        avg(case 
            when completed_at is not null and estimated_delivery_minutes is not null
            then abs(duration_minutes - estimated_delivery_minutes)
        end) as avg_eta_deviation_minutes,
        
        -- Customer satisfaction
        avg(rating) filter (where rating is not null) as avg_rating,
        count(*) filter (where rating = 5) as five_star_count,
        count(*) filter (where rating <= 2) as poor_rating_count,
        
        -- Completion rates
        count(*) filter (where status = 'completed' and rating is not null) as rated_rides,
        
        -- Service level metrics
        count(*) filter (where matching_time_seconds <= 30) as rides_matched_under_30s,
        count(*) filter (where matching_time_seconds <= 60) as rides_matched_under_60s,
        count(*) filter (where matching_time_seconds > 300) as rides_matched_over_5min
        
    from {{ ref('stg_rides') }}
    group by 1
),

order_quality as (
    select
        date(created_at) as date,
        count(*) as total_orders,
        count(*) filter (where status = 'delivered') as delivered_orders,
        count(*) filter (where status = 'cancelled') as cancelled_orders,
        
        -- Delivery performance
        avg(actual_delivery_minutes) filter (where actual_delivery_minutes is not null) as avg_delivery_time,
        percentile_cont(0.95) within group (order by actual_delivery_minutes) as p95_delivery_time,
        
        -- On-time delivery
        count(*) filter (where delivery_performance = 'On Time') as on_time_deliveries,
        count(*) filter (where delivery_performance = 'Late') as late_deliveries,
        count(*) filter (where delivery_performance = 'Very Late') as very_late_deliveries,
        
        -- Customer satisfaction
        avg(rating) filter (where rating is not null) as avg_rating,
        count(*) filter (where rating = 5) as five_star_count,
        count(*) filter (where rating <= 2) as poor_rating_count
        
    from {{ ref('stg_orders') }}
    group by 1
),

hotel_quality as (
    select
        date(check_in) as date,
        count(*) as total_bookings,
        count(*) filter (where status in ('confirmed', 'checked_out')) as successful_bookings,
        count(*) filter (where status = 'cancelled') as cancelled_bookings,
        avg(total_amount) as avg_booking_value
        
    from {{ ref('stg_bookings') }}
    group by 1
),

daily_quality as (
    select
        coalesce(rq.date, oq.date, hq.date) as date,
        
        -- Ride metrics
        coalesce(rq.total_rides, 0) as rides_total,
        coalesce(rq.completed_rides, 0) as rides_completed,
        coalesce(rq.cancelled_rides, 0) as rides_cancelled,
        rq.avg_matching_time_seconds,
        rq.p95_matching_time_seconds,
        rq.p99_matching_time_seconds,
        rq.avg_eta_deviation_minutes,
        rq.avg_rating as rides_avg_rating,
        rq.five_star_count as rides_five_star,
        rq.poor_rating_count as rides_poor_rating,
        
        -- Order metrics
        coalesce(oq.total_orders, 0) as orders_total,
        coalesce(oq.delivered_orders, 0) as orders_delivered,
        coalesce(oq.cancelled_orders, 0) as orders_cancelled,
        oq.avg_delivery_time,
        oq.p95_delivery_time,
        oq.on_time_deliveries,
        oq.late_deliveries,
        oq.very_late_deliveries,
        oq.avg_rating as orders_avg_rating,
        oq.five_star_count as orders_five_star,
        oq.poor_rating_count as orders_poor_rating,
        
        -- Hotel metrics
        coalesce(hq.total_bookings, 0) as bookings_total,
        coalesce(hq.successful_bookings, 0) as bookings_successful,
        coalesce(hq.cancelled_bookings, 0) as bookings_cancelled
        
    from ride_quality rq
    full outer join order_quality oq on rq.date = oq.date
    full outer join hotel_quality hq on coalesce(rq.date, oq.date) = hq.date
)

select
    *,
    
    -- Ride SLA compliance
    case 
        when rides_total > 0 then
            round(rides_completed::numeric / rides_total * 100, 2)
        else null
    end as ride_completion_rate_pct,
    
    case 
        when rides_total > 0 then
            round(rides_cancelled::numeric / rides_total * 100, 2)
        else null
    end as ride_cancellation_rate_pct,
    
    -- Matching SLA
    case 
        when rides_total > 0 then
            round(rides_matched_under_30s::numeric / rides_total * 100, 2)
        else null
    end as rides_matched_under_30s_pct,
    
    case 
        when rides_total > 0 then
            round(rides_matched_under_60s::numeric / rides_total * 100, 2)
        else null
    end as rides_matched_under_60s_pct,
    
    -- Order SLA compliance
    case 
        when orders_total > 0 then
            round(orders_delivered::numeric / orders_total * 100, 2)
        else null
    end as order_completion_rate_pct,
    
    case 
        when orders_total > 0 then
            round(on_time_deliveries::numeric / orders_delivered * 100, 2)
        else null
    end as on_time_delivery_rate_pct,
    
    case 
        when orders_total > 0 then
            round(late_deliveries::numeric / orders_delivered * 100, 2)
        else null
    end as late_delivery_rate_pct,
    
    -- Hotel SLA compliance
    case 
        when bookings_total > 0 then
            round(bookings_successful::numeric / bookings_total * 100, 2)
        else null
    end as booking_success_rate_pct,
    
    -- Overall customer satisfaction
    case 
        when (rides_five_star + orders_five_star) > 0 and (rides_total + orders_total) > 0 then
            round(
                (rides_five_star + orders_five_star)::numeric / 
                nullif(rides_completed + orders_delivered, 0) * 100,
                2
            )
        else null
    end as overall_five_star_rate_pct,
    
    -- Service health score (0-100)
    round(
        coalesce(
            (
                coalesce(rides_completed::numeric / nullif(rides_total, 0), 1) * 25 +
                coalesce(on_time_deliveries::numeric / nullif(orders_delivered, 0), 1) * 25 +
                coalesce((5 - coalesce(avg_eta_deviation_minutes, 0)) / 5, 1) * 25 +
                coalesce((rides_five_star::numeric + orders_five_star) / nullif(rides_completed + orders_delivered, 0), 0.8) * 25
            ),
            0
        ),
        2
    ) as service_health_score,
    
    -- Quality tier
    case 
        when coalesce(rides_avg_rating, orders_avg_rating) >= 4.7 then 'Excellent'
        when coalesce(rides_avg_rating, orders_avg_rating) >= 4.3 then 'Good'
        when coalesce(rides_avg_rating, orders_avg_rating) >= 3.8 then 'Fair'
        when coalesce(rides_avg_rating, orders_avg_rating) is not null then 'Poor'
        else 'No Data'
    end as quality_tier
    
from daily_quality
order by date desc
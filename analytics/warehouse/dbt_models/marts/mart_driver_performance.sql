-- ============================================================================
-- Mart Model: Driver Performance Analytics
-- Purpose: Track driver metrics, earnings, and quality
-- ============================================================================

{{
    config(
        materialized='table',
        tags=['mart', 'drivers', 'performance'],
        unique_key='driver_id'
    )
}}

with driver_rides as (
    select
        driver_id,
        count(*) as total_rides,
        count(*) filter (where status = 'completed') as completed_rides,
        count(*) filter (where status = 'cancelled') as cancelled_rides,
        count(distinct date(requested_at)) as active_days,
        sum(fare_amount) filter (where status = 'completed') as total_earnings,
        sum(tip_amount) filter (where status = 'completed') as total_tips,
        sum(distance_km) filter (where status = 'completed') as total_distance_km,
        avg(fare_amount) filter (where status = 'completed') as avg_fare,
        avg(rating) filter (where rating is not null) as avg_rating,
        count(*) filter (where rating = 5) as five_star_rides,
        count(*) filter (where rating <= 3) as low_rating_rides,
        min(requested_at) as first_ride_at,
        max(requested_at) as last_ride_at,
        avg(distance_km) filter (where status = 'completed') as avg_distance_per_ride,
        sum(duration_minutes) filter (where status = 'completed') as total_minutes_driven
    from {{ ref('stg_rides') }}
    where driver_id is not null
    group by 1
),

driver_deliveries as (
    select
        driver_id,
        count(*) as total_deliveries,
        count(*) filter (where status = 'delivered') as completed_deliveries,
        count(*) filter (where status = 'cancelled') as cancelled_deliveries,
        sum(total_amount) filter (where status = 'delivered') as delivery_earnings,
        sum(tip_amount) filter (where status = 'delivered') as delivery_tips,
        avg(rating) filter (where rating is not null) as avg_delivery_rating,
        avg(actual_delivery_minutes) filter (where actual_delivery_minutes is not null) as avg_delivery_time
    from {{ ref('stg_orders') }}
    where driver_id is not null
    group by 1
),

driver_hourly_stats as (
    select
        driver_id,
        date(requested_at) as activity_date,
        extract(hour from requested_at) as activity_hour,
        count(*) as rides_in_hour,
        sum(fare_amount) as earnings_in_hour,
        avg(rating) as avg_rating_in_hour
    from {{ ref('stg_rides') }}
    where driver_id is not null and status = 'completed'
    group by 1, 2, 3
),

peak_hours as (
    select
        driver_id,
        mode() within group (order by activity_hour) as most_active_hour,
        max(rides_in_hour) as max_rides_in_hour,
        avg(rides_in_hour) as avg_rides_per_active_hour
    from driver_hourly_stats
    group by 1
),

driver_cities as (
    select
        driver_id,
        count(distinct split_part(pickup_address, ',', -1)) as cities_served
    from {{ ref('stg_rides') }}
    where driver_id is not null
    group by 1
),

driver_performance as (
    select
        dr.driver_id,
        
        -- Ride metrics
        coalesce(dr.total_rides, 0) as total_rides,
        coalesce(dr.completed_rides, 0) as completed_rides,
        coalesce(dr.cancelled_rides, 0) as cancelled_rides,
        coalesce(dr.active_days, 0) as active_days,
        
        -- Earnings
        coalesce(dr.total_earnings, 0) as ride_earnings,
        coalesce(dr.total_tips, 0) as ride_tips,
        coalesce(dr.total_earnings, 0) + coalesce(dr.total_tips, 0) as total_ride_compensation,
        
        -- Delivery metrics
        coalesce(dd.total_deliveries, 0) as total_deliveries,
        coalesce(dd.completed_deliveries, 0) as completed_deliveries,
        coalesce(dd.delivery_earnings, 0) as delivery_earnings,
        coalesce(dd.delivery_tips, 0) as delivery_tips,
        
        -- Combined earnings
        coalesce(dr.total_earnings, 0) + coalesce(dr.total_tips, 0) +
        coalesce(dd.delivery_earnings, 0) + coalesce(dd.delivery_tips, 0) as total_earnings,
        
        -- Quality metrics
        coalesce(dr.avg_rating, dd.avg_delivery_rating) as overall_rating,
        coalesce(dr.five_star_rides, 0) as five_star_rides,
        coalesce(dr.low_rating_rides, 0) as low_rating_rides,
        
        -- Distance & time
        coalesce(dr.total_distance_km, 0) as total_distance_km,
        coalesce(dr.total_minutes_driven, 0) as total_minutes_driven,
        coalesce(dr.avg_distance_per_ride, 0) as avg_distance_per_ride,
        coalesce(dr.avg_fare, 0) as avg_fare,
        coalesce(dd.avg_delivery_time, 0) as avg_delivery_time,
        
        -- Activity
        dr.first_ride_at,
        dr.last_ride_at,
        date_diff('day', dr.first_ride_at, current_date) as days_as_driver,
        date_diff('day', dr.last_ride_at, current_date) as days_since_last_activity,
        
        -- Peak hours
        ph.most_active_hour,
        ph.max_rides_in_hour,
        ph.avg_rides_per_active_hour,
        
        -- Geographic reach
        coalesce(dc.cities_served, 1) as cities_served,
        
        -- Calculated metrics
        case 
            when dr.total_rides > 0 then
                round(dr.completed_rides::numeric / dr.total_rides * 100, 2)
            else 0
        end as completion_rate,
        
        case 
            when dr.active_days > 0 then
                round(dr.total_rides::numeric / dr.active_days, 2)
            else 0
        end as avg_rides_per_day,
        
        case 
            when dr.total_minutes_driven > 0 then
                round(
                    (coalesce(dr.total_earnings, 0) + coalesce(dr.total_tips, 0)) / 
                    (dr.total_minutes_driven / 60.0),
                    2
                )
            else 0
        end as hourly_earnings,
        
        -- Performance tier
        case 
            when coalesce(dr.avg_rating, dd.avg_delivery_rating) >= 4.8 
                 and coalesce(dr.total_rides, 0) >= 100 then 'Elite'
            when coalesce(dr.avg_rating, dd.avg_delivery_rating) >= 4.5 
                 and coalesce(dr.total_rides, 0) >= 50 then 'Premium'
            when coalesce(dr.avg_rating, dd.avg_delivery_rating) >= 4.0 
                 and coalesce(dr.total_rides, 0) >= 20 then 'Standard'
            when coalesce(dr.total_rides, 0) > 0 then 'Basic'
            else 'New'
        end as performance_tier,
        
        -- Churn risk
        case 
            when date_diff('day', dr.last_ride_at, current_date) > 60 then 'Inactive'
            when date_diff('day', dr.last_ride_at, current_date) > 30 then 'At Risk'
            else 'Active'
        end as driver_status
    
    from driver_rides dr
    left join driver_deliveries dd on dr.driver_id = dd.driver_id
    left join peak_hours ph on dr.driver_id = ph.driver_id
    left join driver_cities dc on dr.driver_id = dc.driver_id
)

select
    *,
    
    -- Service diversity
    case 
        when total_rides > 0 and total_deliveries > 0 then 'Multi-Service'
        when total_rides > 0 then 'Rides Only'
        when total_deliveries > 0 then 'Delivery Only'
        else 'Inactive'
    end as service_type,
    
    -- Revenue per rating point
    case 
        when overall_rating > 0 then
            round(total_earnings / overall_rating, 2)
        else 0
    end as revenue_per_rating_point,
    
    -- Experience level
    case 
        when days_as_driver >= 365 then 'Veteran (1+ year)'
        when days_as_driver >= 180 then 'Experienced (6+ months)'
        when days_as_driver >= 90 then 'Established (3+ months)'
        when days_as_driver >= 30 then 'Regular (1+ month)'
        else 'New (< 1 month)'
    end as experience_level
    
from driver_performance
order by total_earnings desc
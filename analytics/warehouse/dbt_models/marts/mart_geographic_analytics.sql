-- ============================================================================
-- Mart Model: Geographic Analytics
-- Purpose: Analyze performance by city, region, and country
-- ============================================================================

{{
    config(
        materialized='table',
        tags=['mart', 'geographic', 'cities']
    )
}}

with city_rides as (
    select
        split_part(pickup_address, ',', -1) as city,
        date(requested_at) as date,
        count(*) as total_rides,
        count(*) filter (where status = 'completed') as completed_rides,
        sum(fare_amount) filter (where status = 'completed') as revenue,
        count(distinct user_id) as unique_riders,
        count(distinct driver_id) as unique_drivers,
        avg(fare_amount) filter (where status = 'completed') as avg_fare,
        avg(rating) filter (where rating is not null) as avg_rating,
        avg(matching_time_seconds) as avg_matching_time
    from {{ ref('stg_rides') }}
    where status = 'completed'
    group by 1, 2
),

city_orders as (
    select
        city,
        date(created_at) as date,
        count(*) as total_orders,
        count(*) filter (where status = 'delivered') as delivered_orders,
        sum(total_amount) filter (where status = 'delivered') as revenue,
        count(distinct user_id) as unique_customers,
        count(distinct restaurant_id) as unique_restaurants,
        avg(total_amount) filter (where status = 'delivered') as avg_order_value,
        avg(actual_delivery_minutes) as avg_delivery_time
    from {{ ref('stg_orders') }}
    where status = 'delivered' and city is not null
    group by 1, 2
),

city_hotels as (
    select
        city,
        date(check_in) as date,
        count(*) as total_bookings,
        sum(total_amount) as revenue,
        count(distinct user_id) as unique_guests,
        count(distinct hotel_id) as unique_hotels,
        avg(total_amount) as avg_booking_value
    from {{ ref('stg_bookings') }}
    where status in ('confirmed', 'checked_out')
    group by 1, 2
),

city_daily as (
    select
        coalesce(cr.city, co.city, ch.city) as city,
        coalesce(cr.date, co.date, ch.date) as date,
        
        -- Rides
        coalesce(cr.total_rides, 0) as rides_count,
        coalesce(cr.revenue, 0) as rides_revenue,
        coalesce(cr.unique_riders, 0) as rides_unique_users,
        coalesce(cr.unique_drivers, 0) as rides_unique_drivers,
        cr.avg_fare as rides_avg_fare,
        cr.avg_rating as rides_avg_rating,
        cr.avg_matching_time as rides_avg_matching_time,
        
        -- Orders
        coalesce(co.total_orders, 0) as orders_count,
        coalesce(co.revenue, 0) as orders_revenue,
        coalesce(co.unique_customers, 0) as orders_unique_users,
        coalesce(co.unique_restaurants, 0) as orders_unique_restaurants,
        co.avg_order_value as orders_avg_value,
        co.avg_delivery_time as orders_avg_delivery_time,
        
        -- Hotels
        coalesce(ch.total_bookings, 0) as hotels_count,
        coalesce(ch.revenue, 0) as hotels_revenue,
        coalesce(ch.unique_guests, 0) as hotels_unique_guests,
        coalesce(ch.unique_hotels, 0) as hotels_unique_hotels,
        ch.avg_booking_value as hotels_avg_value
        
    from city_rides cr
    full outer join city_orders co on cr.city = co.city and cr.date = co.date
    full outer join city_hotels ch on coalesce(cr.city, co.city) = ch.city 
                                      and coalesce(cr.date, co.date) = ch.date
),

city_summary as (
    select
        city,
        
        -- Aggregate metrics
        sum(rides_count) as total_rides,
        sum(orders_count) as total_orders,
        sum(hotels_count) as total_hotel_bookings,
        sum(rides_count) + sum(orders_count) + sum(hotels_count) as total_transactions,
        
        sum(rides_revenue) as total_rides_revenue,
        sum(orders_revenue) as total_orders_revenue,
        sum(hotels_revenue) as total_hotels_revenue,
        sum(rides_revenue) + sum(orders_revenue) + sum(hotels_revenue) as total_revenue,
        
        -- User metrics
        count(distinct rides_unique_users) + count(distinct orders_unique_users) as total_unique_users,
        count(distinct rides_unique_drivers) as total_drivers,
        count(distinct orders_unique_restaurants) as total_restaurants,
        count(distinct hotels_unique_hotels) as total_hotels,
        
        -- Averages
        avg(rides_avg_fare) as avg_ride_fare,
        avg(orders_avg_value) as avg_order_value,
        avg(hotels_avg_value) as avg_hotel_booking,
        avg(rides_avg_rating) as avg_service_rating,
        avg(rides_avg_matching_time) as avg_matching_time,
        avg(orders_avg_delivery_time) as avg_delivery_time,
        
        -- Activity metrics
        count(distinct date) as active_days,
        min(date) as first_activity_date,
        max(date) as last_activity_date,
        
        -- Growth metrics
        sum(rides_count) filter (where date >= current_date - interval '7 days') as rides_last_7_days,
        sum(rides_count) filter (where date >= current_date - interval '30 days') as rides_last_30_days,
        sum(orders_count) filter (where date >= current_date - interval '7 days') as orders_last_7_days,
        sum(orders_count) filter (where date >= current_date - interval '30 days') as orders_last_30_days
        
    from city_daily
    where city is not null
    group by 1
)

select
    *,
    
    -- Revenue per user
    case 
        when total_unique_users > 0 then
            round(total_revenue / total_unique_users, 2)
        else 0
    end as revenue_per_user,
    
    -- Transactions per user
    case 
        when total_unique_users > 0 then
            round(total_transactions::numeric / total_unique_users, 2)
        else 0
    end as transactions_per_user,
    
    -- Service diversity score
    case 
        when total_rides > 0 and total_orders > 0 and total_hotel_bookings > 0 then 3
        when (total_rides > 0 and total_orders > 0) or 
             (total_rides > 0 and total_hotel_bookings > 0) or 
             (total_orders > 0 and total_hotel_bookings > 0) then 2
        when total_rides > 0 or total_orders > 0 or total_hotel_bookings > 0 then 1
        else 0
    end as service_diversity_score,
    
    -- Market maturity
    case 
        when date_diff('day', first_activity_date, current_date) >= 365 then 'Mature'
        when date_diff('day', first_activity_date, current_date) >= 180 then 'Growing'
        when date_diff('day', first_activity_date, current_date) >= 90 then 'Emerging'
        when date_diff('day', first_activity_date, current_date) >= 30 then 'New'
        else 'Launch'
    end as market_maturity,
    
    -- Growth rate (7-day vs 30-day average)
    case 
        when rides_last_30_days > 0 then
            round(
                ((rides_last_7_days / 7.0) / (rides_last_30_days / 30.0) - 1) * 100,
                2
            )
        else null
    end as rides_growth_rate_pct,
    
    -- City tier
    case 
        when total_revenue >= 1000000 then 'Tier 1 - Major'
        when total_revenue >= 500000 then 'Tier 2 - Large'
        when total_revenue >= 100000 then 'Tier 3 - Medium'
        when total_revenue >= 10000 then 'Tier 4 - Small'
        else 'Tier 5 - Micro'
    end as city_tier,
    
    -- Operational efficiency
    case 
        when total_drivers > 0 and total_rides > 0 then
            round(total_rides::numeric / total_drivers, 2)
        else 0
    end as rides_per_driver,
    
    -- Market penetration indicator
    case 
        when total_unique_users >= 10000 and total_transactions >= 50000 then 'High Penetration'
        when total_unique_users >= 1000 and total_transactions >= 5000 then 'Medium Penetration'
        when total_unique_users >= 100 and total_transactions >= 500 then 'Low Penetration'
        else 'Early Stage'
    end as market_penetration
    
from city_summary
order by total_revenue desc
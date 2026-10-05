-- ============================================================================
-- Mart Model: User Lifecycle Analytics
-- Purpose: Track user behavior, retention, and lifetime value
-- ============================================================================

{{
    config(
        materialized='table',
        tags=['mart', 'users', 'lifecycle'],
        unique_key='user_id'
    )
}}

with user_first_activity as (
    select
        user_id,
        min(requested_at) as first_ride_at,
        min(created_at) as first_order_at,
        min(created_at) as account_created_at,
        count(distinct case when status = 'completed' then ride_id end) as total_rides,
        count(distinct case when status = 'delivered' then order_id end) as total_orders
    from (
        select user_id, requested_at, ride_id, status, 'ride' as activity_type, created_at
        from {{ ref('stg_rides') }}
        union all
        select user_id, created_at as requested_at, order_id as ride_id, status, 'order' as activity_type, created_at
        from {{ ref('stg_orders') }}
    ) combined
    group by 1
),

user_rides_stats as (
    select
        user_id,
        count(*) as rides_count,
        sum(fare_amount) as total_spend_rides,
        avg(fare_amount) as avg_ride_fare,
        avg(rating) as avg_ride_rating,
        count(distinct date(requested_at)) as active_days_rides,
        min(requested_at) as first_ride_at,
        max(requested_at) as last_ride_at,
        count(distinct ride_id) filter (where status = 'cancelled') as cancelled_rides
    from {{ ref('stg_rides') }}
    group by 1
),

user_orders_stats as (
    select
        user_id,
        count(*) as orders_count,
        sum(total_amount) as total_spend_orders,
        avg(total_amount) as avg_order_value,
        avg(rating) as avg_order_rating,
        count(distinct date(created_at)) as active_days_orders,
        min(created_at) as first_order_at,
        max(created_at) as last_order_at,
        count(distinct order_id) filter (where status = 'cancelled') as cancelled_orders
    from {{ ref('stg_orders') }}
    group by 1
),

user_bookings_stats as (
    select
        user_id,
        count(*) as bookings_count,
        sum(total_amount) as total_spend_hotels,
        avg(total_amount) as avg_booking_value,
        count(distinct hotel_id) as unique_hotels,
        count(distinct city) as unique_cities_visited
    from {{ ref('stg_bookings') }}
    where status in ('confirmed', 'checked_out')
    group by 1
),

user_cohort as (
    select
        user_id,
        date_trunc('month', account_created_at) as cohort_month,
        date_part('year', account_created_at) as cohort_year,
        date_part('month', account_created_at) as cohort_month_num
    from user_first_activity
),

user_segments as (
    select
        ufa.user_id,
        
        -- Engagement metrics
        coalesce(rs.rides_count, 0) + coalesce(os.orders_count, 0) as total_transactions,
        coalesce(rs.total_spend_rides, 0) + coalesce(os.total_spend_orders, 0) + coalesce(bs.total_spend_hotels, 0) as lifetime_value,
        
        -- Activity recency
        greatest(
            coalesce(rs.last_ride_at, '1970-01-01'::timestamp),
            coalesce(os.last_order_at, '1970-01-01'::timestamp)
        ) as last_activity_at,
        
        -- Days since last activity
        date_diff('day', 
            greatest(
                coalesce(rs.last_ride_at, '1970-01-01'::timestamp),
                coalesce(os.last_order_at, '1970-01-01'::timestamp)
            ),
            current_date
        ) as days_since_last_activity,
        
        -- Account age
        date_diff('day', ufa.account_created_at, current_date) as account_age_days,
        
        -- Multi-service usage
        case 
            when rs.rides_count > 0 and os.orders_count > 0 and bs.bookings_count > 0 then 'Multi-Service'
            when rs.rides_count > 0 and os.orders_count > 0 then 'Rides + Food'
            when rs.rides_count > 0 and bs.bookings_count > 0 then 'Rides + Hotels'
            when os.orders_count > 0 and bs.bookings_count > 0 then 'Food + Hotels'
            when rs.rides_count > 0 then 'Rides Only'
            when os.orders_count > 0 then 'Food Only'
            when bs.bookings_count > 0 then 'Hotels Only'
            else 'Inactive'
        end as service_usage_pattern,
        
        -- User value tier
        case 
            when coalesce(rs.total_spend_rides, 0) + coalesce(os.total_spend_orders, 0) + coalesce(bs.total_spend_hotels, 0) >= 5000 then 'Platinum'
            when coalesce(rs.total_spend_rides, 0) + coalesce(os.total_spend_orders, 0) + coalesce(bs.total_spend_hotels, 0) >= 1000 then 'Gold'
            when coalesce(rs.total_spend_rides, 0) + coalesce(os.total_spend_orders, 0) + coalesce(bs.total_spend_hotels, 0) >= 250 then 'Silver'
            when coalesce(rs.total_spend_rides, 0) + coalesce(os.total_spend_orders, 0) + coalesce(bs.total_spend_hotels, 0) > 0 then 'Bronze'
            else 'Inactive'
        end as user_tier,
        
        -- Churn risk
        case 
            when date_diff('day', 
                greatest(
                    coalesce(rs.last_ride_at, '1970-01-01'::timestamp),
                    coalesce(os.last_order_at, '1970-01-01'::timestamp)
                ),
                current_date
            ) > 90 then 'High Risk'
            when date_diff('day', 
                greatest(
                    coalesce(rs.last_ride_at, '1970-01-01'::timestamp),
                    coalesce(os.last_order_at, '1970-01-01'::timestamp)
                ),
                current_date
            ) > 30 then 'Medium Risk'
            else 'Low Risk'
        end as churn_risk,
        
        -- Raw metrics
        rs.*,
        os.*,
        bs.*
    
    from user_first_activity ufa
    left join user_rides_stats rs on ufa.user_id = rs.user_id
    left join user_orders_stats os on ufa.user_id = os.user_id
    left join user_bookings_stats bs on ufa.user_id = bs.user_id
)

select
    us.*,
    uc.cohort_month,
    uc.cohort_year,
    uc.cohort_month_num,
    
    -- Calculated metrics
    case 
        when us.account_age_days > 0 then
            round(us.lifetime_value / us.account_age_days, 2)
        else 0
    end as avg_daily_spend,
    
    case 
        when us.total_transactions > 0 then
            round(us.lifetime_value / us.total_transactions, 2)
        else 0
    end as avg_transaction_value,
    
    -- Retention flags
    case 
        when us.total_transactions >= 5 and us.days_since_last_activity <= 30 then 'Active Power User'
        when us.total_transactions >= 2 and us.days_since_last_activity <= 60 then 'Active Regular'
        when us.total_transactions >= 1 and us.days_since_last_activity <= 90 then 'Active Occasional'
        when us.days_since_last_activity > 90 then 'Churned'
        else 'New User'
    end as user_status,
    
    -- Predicted LTV (simple model based on average spend and tenure)
    case 
        when us.account_age_days > 0 then
            round(
                (us.lifetime_value / us.account_age_days) * 365,
                2
            )
        else 0
    end as predicted_annual_ltv
    
from user_segments us
left join user_cohort uc on us.user_id = uc.user_id
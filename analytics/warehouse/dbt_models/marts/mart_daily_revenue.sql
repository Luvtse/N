-- ============================================================================
-- Mart Model: Daily Revenue
-- Purpose: Aggregate daily revenue across all services
-- ============================================================================

{{
    config(
        materialized='table',
        tags=['mart', 'revenue', 'daily']
    )
}}

with rides_revenue as (
    select
        ride_date as date,
        'rides' as service,
        count(*) as transaction_count,
        sum(fare_amount) as gross_revenue,
        sum(tip_amount) as tips,
        sum(fare_amount * 0.20) as platform_fee,  -- 20% commission
        avg(fare_amount) as avg_transaction_value,
        count(distinct user_id) as unique_customers,
        count(distinct driver_id) as unique_providers
    from {{ ref('stg_rides') }}
    where status = 'completed'
    group by 1
),

hotels_revenue as (
    select
        date(check_in) as date,
        'hotels' as service,
        count(*) as transaction_count,
        sum(total_amount) as gross_revenue,
        0 as tips,
        sum(total_amount * 0.15) as platform_fee,  -- 15% commission
        avg(total_amount) as avg_transaction_value,
        count(distinct user_id) as unique_customers,
        count(distinct hotel_id) as unique_providers
    from {{ ref('stg_bookings') }}
    where status in ('confirmed', 'checked_in', 'checked_out')
    group by 1
),

orders_revenue as (
    select
        order_date as date,
        'food' as service,
        count(*) as transaction_count,
        sum(total_amount) as gross_revenue,
        sum(tip_amount) as tips,
        sum(total_amount * 0.25) as platform_fee,  -- 25% commission
        avg(total_amount) as avg_transaction_value,
        count(distinct user_id) as unique_customers,
        count(distinct restaurant_id) as unique_providers
    from {{ ref('stg_orders') }}
    where status = 'delivered'
    group by 1
),

combined as (
    select * from rides_revenue
    union all
    select * from hotels_revenue
    union all
    select * from orders_revenue
),

daily_summary as (
    select
        date,
        sum(transaction_count) as total_transactions,
        sum(gross_revenue) as total_gross_revenue,
        sum(tips) as total_tips,
        sum(platform_fee) as total_platform_revenue,
        avg(avg_transaction_value) as overall_avg_transaction,
        sum(unique_customers) as total_unique_customers,
        sum(unique_providers) as total_unique_providers
    from combined
    group by 1
)

select
    ds.date,
    ds.total_transactions,
    ds.total_gross_revenue,
    ds.total_tips,
    ds.total_platform_revenue,
    ds.overall_avg_transaction,
    ds.total_unique_customers,
    ds.total_unique_providers,
    
    -- Service breakdown
    coalesce(r.gross_revenue, 0) as rides_revenue,
    coalesce(h.gross_revenue, 0) as hotels_revenue,
    coalesce(o.gross_revenue, 0) as food_revenue,
    
    coalesce(r.transaction_count, 0) as rides_count,
    coalesce(h.transaction_count, 0) as hotels_count,
    coalesce(o.transaction_count, 0) as food_count,
    
    -- Revenue mix percentages
    case 
        when ds.total_gross_revenue > 0 then
            round(coalesce(r.gross_revenue, 0) / ds.total_gross_revenue * 100, 2)
        else 0
    end as rides_revenue_pct,
    
    case 
        when ds.total_gross_revenue > 0 then
            round(coalesce(h.gross_revenue, 0) / ds.total_gross_revenue * 100, 2)
        else 0
    end as hotels_revenue_pct,
    
    case 
        when ds.total_gross_revenue > 0 then
            round(coalesce(o.gross_revenue, 0) / ds.total_gross_revenue * 100, 2)
        else 0
    end as food_revenue_pct,
    
    -- Day-over-day growth
    case 
        when lag(ds.total_gross_revenue) over (order by ds.date) > 0 then
            round(
                (ds.total_gross_revenue - lag(ds.total_gross_revenue) over (order by ds.date))
                / lag(ds.total_gross_revenue) over (order by ds.date) * 100,
                2
            )
        else null
    end as revenue_growth_pct
    
from daily_summary ds
left
-- ============================================================================
-- Mart Model: Daily Revenue
-- Purpose: Aggregate daily revenue across all services
-- ============================================================================

{{
    config(
        materialized='table',
        tags=['mart', 'revenue', 'daily'],
        unique_key='date'
    )
}}

with rides_revenue as (
    select
        ride_date as date,
        'rides' as service,
        count(*) as transaction_count,
        sum(fare_amount) as gross_revenue,
        sum(tip_amount) as tips,
        sum(fare_amount * 0.20) as platform_fee,
        avg(fare_amount) as avg_transaction_value,
        count(distinct user_id) as unique_customers,
        count(distinct driver_id) as unique_providers
    from {{ ref('stg_rides') }}
    where status = 'completed'
    group by 1
),

hotels_revenue as (
    select
        date(check_in) as date,
        'hotels' as service,
        count(*) as transaction_count,
        sum(total_amount) as gross_revenue,
        0 as tips,
        sum(total_amount * 0.15) as platform_fee,
        avg(total_amount) as avg_transaction_value,
        count(distinct user_id) as unique_customers,
        count(distinct hotel_id) as unique_providers
    from {{ ref('stg_bookings') }}
    where status in ('confirmed', 'checked_in', 'checked_out')
    group by 1
),

orders_revenue as (
    select
        order_date as date,
        'food' as service,
        count(*) as transaction_count,
        sum(total_amount) as gross_revenue,
        sum(tip_amount) as tips,
        sum(total_amount * 0.25) as platform_fee,
        avg(total_amount) as avg_transaction_value,
        count(distinct user_id) as unique_customers,
        count(distinct restaurant_id) as unique_providers
    from {{ ref('stg_orders') }}
    where status = 'delivered'
    group by 1
),

combined as (
    select * from rides_revenue
    union all
    select * from hotels_revenue
    union all
    select * from orders_revenue
),

daily_summary as (
    select
        date,
        sum(transaction_count) as total_transactions,
        sum(gross_revenue) as total_gross_revenue,
        sum(tips) as total_tips,
        sum(platform_fee) as total_platform_revenue,
        avg(avg_transaction_value) as overall_avg_transaction,
        count(distinct unique_customers) as total_unique_customers,
        sum(unique_providers) as total_unique_providers
    from combined
    group by 1
)

select
    ds.date,
    ds.total_transactions,
    ds.total_gross_revenue,
    ds.total_tips,
    ds.total_platform_revenue,
    ds.overall_avg_transaction,
    ds.total_unique_customers,
    ds.total_unique_providers,
    
    -- Service breakdown
    coalesce(r.gross_revenue, 0) as rides_revenue,
    coalesce(h.gross_revenue, 0) as hotels_revenue,
    coalesce(o.gross_revenue, 0) as food_revenue,
    
    coalesce(r.transaction_count, 0) as rides_count,
    coalesce(h.transaction_count, 0) as hotels_count,
    coalesce(o.transaction_count, 0) as food_count,
    
    -- Revenue mix percentages
    case 
        when ds.total_gross_revenue > 0 then
            round(coalesce(r.gross_revenue, 0) / ds.total_gross_revenue * 100, 2)
        else 0
    end as rides_revenue_pct,
    
    case 
        when ds.total_gross_revenue > 0 then
            round(coalesce(h.gross_revenue, 0) / ds.total_gross_revenue * 100, 2)
        else 0
    end as hotels_revenue_pct,
    
    case 
        when ds.total_gross_revenue > 0 then
            round(coalesce(o.gross_revenue, 0) / ds.total_gross_revenue * 100, 2)
        else 0
    end as food_revenue_pct,
    
    -- Day-over-day growth
    case 
        when lag(ds.total_gross_revenue) over (order by ds.date) > 0 then
            round(
                (ds.total_gross_revenue - lag(ds.total_gross_revenue) over (order by ds.date))
                / lag(ds.total_gross_revenue) over (order by ds.date) * 100,
                2
            )
        else null
    end as revenue_growth_pct,
    
    -- 7-day moving average
    round(avg(ds.total_gross_revenue) over (
        order by ds.date 
        rows between 6 preceding and current row
    ), 2) as revenue_7day_moving_avg,
    
    -- Week-over-week comparison
    case 
        when lag(ds.total_gross_revenue, 7) over (order by ds.date) > 0 then
            round(
                (ds.total_gross_revenue - lag(ds.total_gross_revenue, 7) over (order by ds.date))
                / lag(ds.total_gross_revenue, 7) over (order by ds.date) * 100,
                2
            )
        else null
    end as revenue_wow_growth_pct
    
from daily_summary ds
left join rides_revenue r on ds.date = r.date
left join hotels_revenue h on ds.date = h.date
left join orders_revenue o on ds.date = o.date
order by ds.date desc
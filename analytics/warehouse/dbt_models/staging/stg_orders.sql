-- ============================================================================
-- Staging Model: Orders
-- Source: raw_vorax.orders
-- Purpose: Clean and standardize food delivery orders
-- ============================================================================

{{
    config(
        materialized='view',
        tags=['staging', 'vorax', 'orders']
    )
}}

with source as (
    select * from {{ source('raw_vorax', 'orders') }}
),

cleaned as (
    select
        -- Primary key
        id as order_id,
        
        -- Foreign keys
        user_id,
        restaurant_id,
        driver_id,
        
        -- Delivery info
        delivery_address,
        delivery_lat,
        delivery_lng,
        
        -- Financial data
        subtotal,
        delivery_fee,
        total_amount,
        tip_amount,
        discount_amount,
        currency,
        payment_method,
        
        -- Status
        status,
        
        -- Metrics
        item_count,
        estimated_delivery_minutes,
        actual_delivery_minutes,
        delivery_distance_km,
        
        -- Feedback
        rating,
        
        -- Audit
        created_at,
        updated_at,
        
        -- Calculated fields
        case 
            when actual_delivery_minutes is not null and estimated_delivery_minutes is not null
            then actual_delivery_minutes - estimated_delivery_minutes
            else null
        end as delivery_delay_minutes,
        
        case 
            when tip_amount > 0 then true
            else false
        end as has_tip,
        
        case 
            when discount_amount > 0 then true
            else false
        end as has_discount,
        
        -- Status categorization
        case 
            when status in ('pending', 'confirmed') then 'new'
            when status in ('preparing', 'ready') then 'preparing'
            when status in ('picked_up', 'in_transit') then 'delivering'
            when status = 'delivered' then 'delivered'
            when status = 'cancelled' then 'cancelled'
            else 'unknown'
        end as order_stage
        
    from source
    where id is not null
),

final as (
    select
        *,
        -- Date dimensions
        date(created_at) as order_date,
        extract(hour from created_at) as order_hour,
        extract(dow from created_at) as day_of_week,
        extract(month from created_at) as order_month,
        extract(year from created_at) as order_year,
        
        -- Meal type
        case 
            when extract(hour from created_at) between 6 and 10 then 'Breakfast'
            when extract(hour from created_at) between 11 and 14 then 'Lunch'
            when extract(hour from created_at) between 15 and 17 then 'Snack'
            when extract(hour from created_at) between 18 and 22 then 'Dinner'
            else 'Late Night'
        end as meal_type,
        
        -- Order value tier
        case 
            when total_amount >= 100 then 'High Value'
            when total_amount >= 50 then 'Medium Value'
            when total_amount >= 25 then 'Standard'
            else 'Low Value'
        end as order_value_tier,
        
        -- Delivery performance
        case 
            when actual_delivery_minutes <= estimated_delivery_minutes then 'On Time'
            when actual_delivery_minutes <= estimated_delivery_minutes + 10 then 'Slightly Late'
            when actual_delivery_minutes <= estimated_delivery_minutes + 20 then 'Late'
            else 'Very Late'
        end as delivery_performance,
        
        -- Rush hour flag
        case 
            when extract(hour from created_at) in (12, 13, 18, 19, 20) then true
            else false
        end as is_rush_hour
        
    from cleaned
)

select * from final
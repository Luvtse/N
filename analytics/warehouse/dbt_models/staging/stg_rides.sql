-- ============================================================================
-- Staging Model: Rides
-- Source: raw_nidus.rides
-- Purpose: Clean and standardize ride data
-- ============================================================================

{{
    config(
        materialized='view',
        tags=['staging', 'nidus', 'rides']
    )
}}

with source as (
    select * from {{ source('raw_nidus', 'rides') }}
),

cleaned as (
    select
        -- Primary key
        id as ride_id,
        
        -- Foreign keys
        user_id,
        driver_id,
        
        -- Location data
        pickup_lat,
        pickup_lng,
        dropoff_lat,
        dropoff_lng,
        
        -- Addresses
        pickup_address,
        dropoff_address,
        
        -- Ride details
        ride_type,
        status,
        
        -- Financial data
        fare_amount,
        currency,
        tip_amount,
        
        -- Metrics
        distance_km,
        duration_minutes,
        
        -- Timestamps
        requested_at,
        matched_at,
        started_at,
        completed_at,
        
        -- Feedback
        rating,
        review,
        
        -- Audit
        created_at,
        updated_at,
        
        -- Calculated fields
        case 
            when completed_at is not null and requested_at is not null
            then extract(epoch from (completed_at - requested_at)) / 60
            else null
        end as total_duration_minutes,
        
        case 
            when matched_at is not null and requested_at is not null
            then extract(epoch from (matched_at - requested_at))
            else null
        end as matching_time_seconds,
        
        -- Status categorization
        case 
            when status in ('requested', 'searching') then 'pending'
            when status in ('matched', 'driver_en_route') then 'assigned'
            when status = 'in_progress' then 'active'
            when status = 'completed' then 'completed'
            when status = 'cancelled' then 'cancelled'
            else 'unknown'
        end as status_category,
        
        -- Ride type categorization
        case 
            when ride_type = 'standard' then 'Standard'
            when ride_type = 'premium' then 'Premium'
            when ride_type = 'electric' then 'Electric'
            when ride_type = 'shared' then 'Shared'
            when ride_type = 'wheelchair' then 'Wheelchair Accessible'
            else 'Other'
        end as ride_type_display,
        
        -- Data quality flags
        case when driver_id is null then true else false end as is_unmatched,
        case when fare_amount is null or fare_amount = 0 then true else false end as is_free_ride,
        case when rating is null then true else false end as is_unrated
        
    from source
    where id is not null
),

final as (
    select
        *,
        -- Date dimensions
        date(requested_at) as ride_date,
        extract(hour from requested_at) as ride_hour,
        extract(dow from requested_at) as day_of_week,
        extract(month from requested_at) as ride_month,
        extract(year from requested_at) as ride_year,
        
        -- Time of day categorization
        case 
            when extract(hour from requested_at) between 6 and 11 then 'Morning'
            when extract(hour from requested_at) between 12 and 17 then 'Afternoon'
            when extract(hour from requested_at) between 18 and 22 then 'Evening'
            else 'Night'
        end as time_of_day,
        
        -- Rush hour flag
        case 
            when extract(hour from requested_at) in (7, 8, 9, 17, 18, 19) then true
            else false
        end as is_rush_hour,
        
        -- Weekend flag
        case 
            when extract(dow from requested_at) in (0, 6) then true
            else false
        end as is_weekend
        
    from cleaned
)

select * from final
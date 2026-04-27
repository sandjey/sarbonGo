--
-- PostgreSQL database dump
--


-- Dumped from database version 18.3
-- Dumped by pg_dump version 18.3

SET statement_timeout = 0;
SET lock_timeout = 0;
SET idle_in_transaction_session_timeout = 0;
SET transaction_timeout = 0;
SET client_encoding = 'UTF8';
SET standard_conforming_strings = on;
SELECT pg_catalog.set_config('search_path', '', false);
SET check_function_bodies = false;
SET xmloption = content;
SET client_min_messages = warning;
SET row_security = off;

SET default_tablespace = '';

SET default_table_access_method = heap;

--
-- Name: cargo_manager_dm_offers; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.cargo_manager_dm_offers (
    id uuid DEFAULT public.uuid_generate_v4() NOT NULL,
    cargo_id uuid NOT NULL,
    cargo_manager_id uuid NOT NULL,
    driver_manager_id uuid NOT NULL,
    driver_id uuid,
    offer_id uuid,
    price double precision NOT NULL,
    currency character varying NOT NULL,
    comment character varying,
    status character varying DEFAULT 'PENDING'::character varying NOT NULL,
    rejection_reason text,
    created_at timestamp without time zone DEFAULT now() NOT NULL,
    updated_at timestamp without time zone DEFAULT now() NOT NULL,
    CONSTRAINT cargo_manager_dm_offers_status_check CHECK (((status)::text = ANY ((ARRAY['PENDING'::character varying, 'ACCEPTED'::character varying, 'REJECTED'::character varying, 'CANCELED'::character varying])::text[])))
);


--
-- Name: cargo_manager_dm_offers cargo_manager_dm_offers_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.cargo_manager_dm_offers
    ADD CONSTRAINT cargo_manager_dm_offers_pkey PRIMARY KEY (id);


--
-- Name: idx_cm_dm_offers_cargo_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_cm_dm_offers_cargo_id ON public.cargo_manager_dm_offers USING btree (cargo_id, status, created_at DESC);


--
-- Name: idx_cm_dm_offers_cm_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_cm_dm_offers_cm_id ON public.cargo_manager_dm_offers USING btree (cargo_manager_id, status, created_at DESC);


--
-- Name: idx_cm_dm_offers_dm_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_cm_dm_offers_dm_id ON public.cargo_manager_dm_offers USING btree (driver_manager_id, status, created_at DESC);


--
-- Name: cargo_manager_dm_offers cargo_manager_dm_offers_cargo_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.cargo_manager_dm_offers
    ADD CONSTRAINT cargo_manager_dm_offers_cargo_id_fkey FOREIGN KEY (cargo_id) REFERENCES public.cargo(id) ON DELETE CASCADE;


--
-- Name: cargo_manager_dm_offers cargo_manager_dm_offers_cargo_manager_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.cargo_manager_dm_offers
    ADD CONSTRAINT cargo_manager_dm_offers_cargo_manager_id_fkey FOREIGN KEY (cargo_manager_id) REFERENCES public.freelance_dispatchers(id) ON DELETE CASCADE;


--
-- Name: cargo_manager_dm_offers cargo_manager_dm_offers_driver_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.cargo_manager_dm_offers
    ADD CONSTRAINT cargo_manager_dm_offers_driver_id_fkey FOREIGN KEY (driver_id) REFERENCES public.drivers(id) ON DELETE SET NULL;


--
-- Name: cargo_manager_dm_offers cargo_manager_dm_offers_driver_manager_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.cargo_manager_dm_offers
    ADD CONSTRAINT cargo_manager_dm_offers_driver_manager_id_fkey FOREIGN KEY (driver_manager_id) REFERENCES public.freelance_dispatchers(id) ON DELETE CASCADE;


--
-- Name: cargo_manager_dm_offers cargo_manager_dm_offers_offer_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.cargo_manager_dm_offers
    ADD CONSTRAINT cargo_manager_dm_offers_offer_id_fkey FOREIGN KEY (offer_id) REFERENCES public.offers(id) ON DELETE SET NULL;


--
-- PostgreSQL database dump complete
--


